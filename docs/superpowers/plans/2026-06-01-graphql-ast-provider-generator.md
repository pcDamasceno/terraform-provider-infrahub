# GraphQL-AST + SDK-Introspection Provider Generator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fork the `marcom4rtinez/infrahub-terraform-provider-generator` so its **data-source** path is driven by a real GraphQL AST plus `go/types` introspection of the genqlient-generated SDK, producing a provider that compiles and faithfully exposes nested lists and polymorphic (inline-fragment) relationship fields — making `make all` actually work for `gql/firewall_policy_rules.gql`.

**Architecture:** Run genqlient **first** so the SDK Go types exist. The forked generator (a) parses each `.gql` into a selection-tree IR with `github.com/vektah/gqlparser/v2`, (b) loads the SDK package via `golang.org/x/tools/go/packages` and walks the response Go type in lockstep with the IR using `go/types` — reading real field names, real scalar Go kinds, and (at interface points) enumerating concrete implementers via `types.Implements` — and (c) emits a recursive Terraform schema + model + `Read` with `switch n := node.(type)` blocks for unions. The legacy **resource** path is left untouched.

**Tech Stack:** Go 1.23, `github.com/vektah/gqlparser/v2` (already in the SDK dep tree), `golang.org/x/tools/go/packages` + `go/types`, `text/template`, terraform-plugin-framework.

---

## Why a rewrite, not a patch (context for the implementer)

The upstream generator (`pkg/parser/parser.go`) is a line scanner. Three facts it cannot represent and which this plan fixes:

1. **Inline fragments.** A line like `... on SecurityIPAddress { name { value } address { value } }` is taken as a scalar field literally named `...` → invalid Go (`Node....`). genqlient instead emits an **interface** `...SecurityGenericAddress` (method `GetTypename()`) with concrete implementers `...NodeSecurityIPAddress`, `...NodeSecurityPrefix`, etc. Concrete-only fields (`address`, `prefix`, `port`) require a Go type-switch.
2. **Lists.** The query returns many rules and many addresses per rule (genqlient emits `[]...Edges`), but the template hard-codes `Edges[0]` and asserts exactly one edge.
3. **Function-name casing.** The template calls `infrahub_sdk.Firewallpolicyrules(...)` via `cases.Title` (lowercases the tail), but genqlient names the function `FirewallPolicyRules` (verbatim operation name, SDK line 1606). Introspection reads the real name, eliminating the guess.

Verification gate for every code task: regenerate, then `go build ./...` from the provider repo root must exit 0 (the SDK types in `sdk/generated_graphql_client.go` are already present and compile, so this is a real, offline acceptance test — no live Infrahub needed).

## Ground-truth SDK symbols (verified, use verbatim)

- Operation fn: `func FirewallPolicyRules(ctx_ context.Context, client_ graphql.Client, policy string) (*FirewallPolicyRulesResponse, error)`
- Response: `type FirewallPolicyRulesResponse struct { SecurityPolicyRule FirewallPolicyRulesSecurityPolicyRulePaginatedSecurityPolicyRule }`
- Polymorphic interface: `...NodeSecurityPolicyRuleSource_addressNestedPaginatedSecurityGenericAddressEdgesNestedEdgedSecurityGenericAddressNodeSecurityGenericAddress` with method `GetTypename() string`.
- Concrete implementers end in `...NodeSecurity<Concrete>` (e.g. `...NodeSecurityIPAddress` with fields `Typename string`, `Name ...NameTextAttribute{Value string}`, `Address ...AddressIPHost{Value string}`).
- SDK is its own module `sdk/go.mod` (`module github.com/opsmill/infrahub-sdk-go/infrahub_sdk`), package `infrahub_sdk`, replaced into the main module via `replace github.com/opsmill/infrahub-sdk-go => ./sdk`.

## File Structure

The fork lives in-repo as its own module so its tooling deps (`go/packages`) never enter the provider's `go.mod`.

- `provider-generator/go.mod` — fork module `module github.com/<you>/infrahub-terraform-provider-generator`, go 1.23, requires gqlparser + x/tools + x/text.
- `provider-generator/cmd/generator/main.go` — copied from upstream; add `-sdk-dir` flag; route `.gql` → data-source via the new pipeline, resources via the old one.
- `provider-generator/pkg/parser/*.go` — copied upstream files; **`parser.go` data-source branch is replaced** by calls into the new packages. Resource branch untouched.
- `provider-generator/pkg/queryir/queryir.go` — **new**: AST → selection-tree IR (gqlparser).
- `provider-generator/pkg/sdkintrospect/sdkintrospect.go` — **new**: load SDK pkg, walk Go types in lockstep with IR, resolve accessors / scalar kinds / union variants.
- `provider-generator/pkg/emit/datasource.go` + `datasource.tmpl` — **new**: recursive schema/model/Read emitter.
- `provider-generator/pkg/*/testdata/` — minimal `.gql` + a checked-in copy of the real generated SDK file for introspection tests.
- `GNUmakefile` — reorder `all`, repoint `automatic_generator`.
- `blog/part-2-build-the-provider.md` — document the reorder + fork.

---

### Task 0: Fork the generator in-repo and wire the build

**Files:**
- Create: `provider-generator/` (copy of the upstream module from `$(go env GOMODCACHE)/github.com/marcom4rtinez/infrahub-terraform-provider-generator@v0.0.0-20250107202910-97783d7b34c5`)
- Create: `provider-generator/go.mod`
- Modify: `GNUmakefile:3` (`all` target order) and `GNUmakefile:22-23` (`automatic_generator`)

- [ ] **Step 1: Copy upstream source into the repo**

```bash
SRC="$(go env GOMODCACHE)/github.com/marcom4rtinez/infrahub-terraform-provider-generator@v0.0.0-20250107202910-97783d7b34c5"
mkdir -p provider-generator
cp -r "$SRC"/cmd "$SRC"/pkg provider-generator/
chmod -R u+w provider-generator   # module cache is read-only
```

- [ ] **Step 2: Create the fork module file**

`provider-generator/go.mod`:
```
module github.com/marcom4rtinez/infrahub-terraform-provider-generator

go 1.23.3

require (
	github.com/vektah/gqlparser/v2 v2.5.11
	golang.org/x/text v0.21.0
	golang.org/x/tools v0.27.0
)
```
(Keep the original module path so internal imports `github.com/marcom4rtinez/.../pkg/templates` resolve unchanged.)

- [ ] **Step 3: Tidy and smoke-build the fork**

Run: `cd provider-generator && go mod tidy && go build ./... && cd ..`
Expected: builds clean (it's still the upstream code at this point).

- [ ] **Step 4: Reorder `make all` so the SDK exists before generation, and repoint the generator**

`GNUmakefile` — change line 3 from:
```
all: automatic_generator generate_sdk fmt lint install generate set_tag
```
to (SDK first; generator second):
```
all: generate_sdk automatic_generator fmt lint install generate set_tag
```
And change the `automatic_generator` target (lines 22-23) from:
```
automatic_generator:
	go run github.com/marcom4rtinez/infrahub-terraform-provider-generator/cmd/generator@latest --artifacts
```
to (run the in-repo fork with explicit paths; deps stay out of the provider go.mod):
```
automatic_generator:
	cd provider-generator && go run ./cmd/generator --artifacts -gql-dir ../gql -provider-dir ../internal/provider -sdk-dir ../sdk
```

- [ ] **Step 5: Commit**

```bash
git add provider-generator GNUmakefile
git commit -m "chore: fork provider generator in-repo, run genqlient before generation"
```

---

### Task 1: GraphQL AST → selection-tree IR

Collapse Infrahub's wrapper layers so the IR mirrors the *Terraform* shape: a `{ value }` chain becomes a scalar leaf named after its parent; `edges { node { ... } }` becomes a List whose children are the node's selections; an inline-fragment set becomes a Union.

**Files:**
- Create: `provider-generator/pkg/queryir/queryir.go`
- Test: `provider-generator/pkg/queryir/queryir_test.go`
- Test data: `provider-generator/pkg/queryir/testdata/firewall.gql` (copy of `gql/firewall_policy_rules.gql`)

- [ ] **Step 1: Write the IR types and parser**

`provider-generator/pkg/queryir/queryir.go`:
```go
package queryir

import (
	"fmt"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

type Kind int

const (
	Scalar Kind = iota // a single leaf value (collapsed `{ value }`)
	Object             // a single nested object (e.g. source_zone { node { name } })
	List               // edges { node { ... } } -> repeated
	Union              // node with inline fragments -> polymorphic
)

type Node struct {
	GqlField string  // graphql field name on its parent ("source_address", "name", "action")
	TFName   string  // terraform attribute name (defaults to GqlField)
	Kind     Kind
	Children []*Node // Object/List members
	Variants []*Variant
}

type Variant struct {
	TypeCond string  // graphql concrete type, e.g. "SecurityIPAddress"
	Children []*Node // fields selected within `... on TypeCond { ... }`
}

type Query struct {
	OpName     string // operation name verbatim: "FirewallPolicyRules"
	RootObject string // top response field: "SecurityPolicyRule"
	VarName    string // single required var: "policy" ("" if none)
	Root       *Node  // RootObject as a List/Object node
	BaseName   string // lower-first op name for tf type/file names: "firewallPolicyRules"
}

// childByName returns the single child selection field with the given name, or nil.
func childByName(ss ast.SelectionSet, name string) *ast.Field {
	for _, sel := range ss {
		if f, ok := sel.(*ast.Field); ok && f.Name == name {
			return f
		}
	}
	return nil
}

func hasInlineFragments(ss ast.SelectionSet) bool {
	for _, sel := range ss {
		if _, ok := sel.(*ast.InlineFragment); ok {
			return true
		}
	}
	return false
}

// onlyValueLeaf reports whether ss is exactly `{ value }`.
func onlyValueLeaf(ss ast.SelectionSet) bool {
	if len(ss) != 1 {
		return false
	}
	f, ok := ss[0].(*ast.Field)
	return ok && f.Name == "value" && len(f.SelectionSet) == 0
}

func Parse(src string) (*Query, error) {
	doc, gerr := parser.ParseQuery(&ast.Source{Input: src})
	if gerr != nil {
		return nil, gerr
	}
	if len(doc.Operations) != 1 {
		return nil, fmt.Errorf("expected exactly 1 operation, got %d", len(doc.Operations))
	}
	op := doc.Operations[0]
	if op.Operation != ast.Query {
		return nil, fmt.Errorf("queryir handles queries only; got %s", op.Operation)
	}

	rootField := op.SelectionSet[0].(*ast.Field) // e.g. SecurityPolicyRule(...)
	q := &Query{
		OpName:     op.Name,
		RootObject: rootField.Name,
		BaseName:   lowerFirst(op.Name),
	}
	if len(op.VariableDefinitions) == 1 {
		q.VarName = op.VariableDefinitions[0].Variable
	}
	q.Root = buildNode(rootField.Name, rootField.SelectionSet)
	return q, nil
}

// buildNode classifies a selection set rooted at a field named gqlField.
func buildNode(gqlField string, ss ast.SelectionSet) *Node {
	n := &Node{GqlField: gqlField, TFName: gqlField}

	// `{ value }` -> scalar leaf.
	if onlyValueLeaf(ss) {
		n.Kind = Scalar
		return n
	}
	// `edges { node { ... } }` -> List of the node's selections.
	if edges := childByName(ss, "edges"); edges != nil {
		node := childByName(edges.SelectionSet, "node")
		n.Kind = List
		if hasInlineFragments(node.SelectionSet) {
			n.Kind = Union
			n.Variants = buildVariants(node.SelectionSet)
			return n
		}
		n.Children = buildChildren(node.SelectionSet)
		return n
	}
	// `node { ... }` (single related object) -> Object of the node's selections.
	if node := childByName(ss, "node"); node != nil {
		n.Kind = Object
		n.Children = buildChildren(node.SelectionSet)
		return n
	}
	// generic object with its own scalar/object children.
	n.Kind = Object
	n.Children = buildChildren(ss)
	return n
}

func buildChildren(ss ast.SelectionSet) []*Node {
	var out []*Node
	for _, sel := range ss {
		f, ok := sel.(*ast.Field)
		if !ok || f.Name == "__typename" {
			continue
		}
		out = append(out, buildNode(f.Name, f.SelectionSet))
	}
	return out
}

func buildVariants(ss ast.SelectionSet) []*Variant {
	var out []*Variant
	for _, sel := range ss {
		frag, ok := sel.(*ast.InlineFragment)
		if !ok {
			continue
		}
		out = append(out, &Variant{
			TypeCond: frag.TypeCondition,
			Children: buildChildren(frag.SelectionSet),
		})
	}
	return out
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]|0x20) + s[1:]
}
```

- [ ] **Step 2: Write the failing test**

`provider-generator/pkg/queryir/queryir_test.go`:
```go
package queryir

import (
	"os"
	"testing"
)

func TestParseFirewall(t *testing.T) {
	src, err := os.ReadFile("testdata/firewall.gql")
	if err != nil {
		t.Fatal(err)
	}
	q, err := Parse(string(src))
	if err != nil {
		t.Fatal(err)
	}
	if q.OpName != "FirewallPolicyRules" {
		t.Errorf("OpName = %q, want FirewallPolicyRules", q.OpName)
	}
	if q.RootObject != "SecurityPolicyRule" {
		t.Errorf("RootObject = %q, want SecurityPolicyRule", q.RootObject)
	}
	if q.VarName != "policy" {
		t.Errorf("VarName = %q, want policy", q.VarName)
	}
	if q.Root.Kind != List {
		t.Fatalf("Root.Kind = %v, want List", q.Root.Kind)
	}
	byName := map[string]*Node{}
	for _, c := range q.Root.Children {
		byName[c.GqlField] = c
	}
	if byName["name"] == nil || byName["name"].Kind != Scalar {
		t.Errorf("name should be a Scalar leaf")
	}
	if byName["source_zone"] == nil || byName["source_zone"].Kind != Object {
		t.Errorf("source_zone should be an Object")
	}
	sa := byName["source_address"]
	if sa == nil || sa.Kind != Union {
		t.Fatalf("source_address should be a Union, got %#v", sa)
	}
	conds := map[string]bool{}
	for _, v := range sa.Variants {
		conds[v.TypeCond] = true
	}
	if !conds["SecurityIPAddress"] || !conds["SecurityPrefix"] {
		t.Errorf("source_address variants = %v, want SecurityIPAddress+SecurityPrefix", conds)
	}
}
```

- [ ] **Step 3: Add test data and run**

```bash
mkdir -p provider-generator/pkg/queryir/testdata
cp gql/firewall_policy_rules.gql provider-generator/pkg/queryir/testdata/firewall.gql
cd provider-generator && go test ./pkg/queryir/ -run TestParseFirewall -v && cd ..
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add provider-generator/pkg/queryir
git commit -m "feat(gen): GraphQL AST -> selection-tree IR with union support"
```

---

### Task 2: SDK introspection via `go/types`

Given the IR and the SDK directory, walk the Go response type in lockstep with the IR. For each scalar, resolve the Go scalar kind (→ tf type) and the read-accessor path. For each union, resolve the interface type and its concrete implementers (`types.Implements`), and per-variant the concrete Go type name + each selected field's accessor.

**Files:**
- Create: `provider-generator/pkg/sdkintrospect/sdkintrospect.go`
- Test: `provider-generator/pkg/sdkintrospect/sdkintrospect_test.go`

- [ ] **Step 1: Write the introspector**

`provider-generator/pkg/sdkintrospect/sdkintrospect.go`:
```go
package sdkintrospect

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/marcom4rtinez/infrahub-terraform-provider-generator/pkg/queryir"
	"golang.org/x/tools/go/packages"
)

// Resolved decorates the IR with Go-level facts needed for emission.
type Resolved struct {
	Query   *queryir.Query
	SDKFunc string // exact exported operation function name, e.g. "FirewallPolicyRules"
	Root    *RNode
}

type RNode struct {
	IR       *queryir.Node
	GoField  string   // Go struct field name on the parent for this selection
	TFType   string   // for Scalar: "types.String"|"types.Int64"|"types.Bool"
	Access   string   // for Scalar: accessor suffix from the *element/object value*, e.g. ".Name.Value"
	Children []*RNode // Object/List members
	Variants []*RVariant
}

type RVariant struct {
	TypeCond    string   // graphql concrete type, e.g. "SecurityIPAddress"
	GoConcrete  string   // concrete genqlient struct name (no pointer), used in `case *X:`
	Children    []*RNode // selected fields resolved against the concrete struct
}

type pkgCtx struct {
	pkg *types.Package
}

func Load(sdkDir string, q *queryir.Query) (*Resolved, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedDeps | packages.NeedImports,
		Dir:  sdkDir,
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 || pkgs[0].Types == nil {
		return nil, fmt.Errorf("could not load SDK package from %s", sdkDir)
	}
	ctx := &pkgCtx{pkg: pkgs[0].Types}

	fn := ctx.pkg.Scope().Lookup(q.OpName)
	if fn == nil {
		return nil, fmt.Errorf("SDK has no exported %s function", q.OpName)
	}
	sig := fn.Type().(*types.Signature)
	// results: (*<OpName>Response, error); deref pointer.
	respPtr := sig.Results().At(0).Type().(*types.Pointer)
	respStruct := respPtr.Elem().Underlying().(*types.Struct)

	// The root object is the single field of the response struct.
	rootGoField, rootType, ok := ctx.fieldByJSONOrName(respStruct, q.RootObject)
	if !ok {
		return nil, fmt.Errorf("response struct has no field for %s", q.RootObject)
	}
	root, err := ctx.resolveNode(q.Root, rootGoField, rootType)
	if err != nil {
		return nil, err
	}
	return &Resolved{Query: q, SDKFunc: q.OpName, Root: root}, nil
}

// resolveNode walks one IR node against the Go type that holds it.
func (c *pkgCtx) resolveNode(n *queryir.Node, goField string, goType types.Type) (*RNode, error) {
	r := &RNode{IR: n, GoField: goField}
	switch n.Kind {
	case queryir.Scalar:
		// goType is the wrapper struct (e.g. ...NameTextAttribute) with a Value field.
		tf, access, err := c.scalarAccess(goType)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n.GqlField, err)
		}
		r.TFType, r.Access = tf, access
		return r, nil

	case queryir.Object:
		// Unwrap `Node` wrapper if present (relationship single-node).
		elem := c.unwrapToStruct(goType)
		for _, child := range n.Children {
			gf, gt, ok := c.fieldByJSONOrName(elem, child.GqlField)
			if !ok {
				return nil, fmt.Errorf("%s has no Go field for %s", goType, child.GqlField)
			}
			rc, err := c.resolveNode(child, gf, gt)
			if err != nil {
				return nil, err
			}
			r.Children = append(r.Children, rc)
		}
		return r, nil

	case queryir.List:
		// goType: ...Paginated{ Edges []...Edged{ Node <T> } }; resolve element node struct.
		nodeStruct := c.edgesNodeStruct(goType)
		for _, child := range n.Children {
			gf, gt, ok := c.fieldByJSONOrName(nodeStruct, child.GqlField)
			if !ok {
				return nil, fmt.Errorf("list element has no Go field for %s", child.GqlField)
			}
			rc, err := c.resolveNode(child, gf, gt)
			if err != nil {
				return nil, err
			}
			r.Children = append(r.Children, rc)
		}
		return r, nil

	case queryir.Union:
		iface := c.edgesNodeInterface(goType) // the *types.Interface for the node
		ifaceNamed := c.edgesNodeNamed(goType)
		for _, v := range n.Variants {
			concrete := c.findImplementer(iface, v.TypeCond)
			if concrete == nil {
				return nil, fmt.Errorf("no SDK implementer of %s for %s", ifaceNamed.Obj().Name(), v.TypeCond)
			}
			rv := &RVariant{TypeCond: v.TypeCond, GoConcrete: concrete.Obj().Name()}
			cs := concrete.Underlying().(*types.Struct)
			for _, child := range v.Children {
				gf, gt, ok := c.fieldByJSONOrName(cs, child.GqlField)
				if !ok {
					return nil, fmt.Errorf("%s has no Go field for %s", concrete.Obj().Name(), child.GqlField)
				}
				rc, err := c.resolveNode(child, gf, gt)
				if err != nil {
					return nil, err
				}
				rv.Children = append(rv.Children, rc)
			}
			r.Variants = append(r.Variants, rv)
		}
		return r, nil
	}
	return nil, fmt.Errorf("unknown IR kind for %s", n.GqlField)
}
```

Plus these `pkgCtx` helpers in the same file:
```go
// fieldByJSONOrName finds a struct field whose json tag (preferred) or name matches gqlName.
func (c *pkgCtx) fieldByJSONOrName(s *types.Struct, gqlName string) (string, types.Type, bool) {
	for i := 0; i < s.NumFields(); i++ {
		f := s.Field(i)
		tag := reflectStructTag(s.Tag(i)).Get("json")
		if name := strings.Split(tag, ",")[0]; name == gqlName {
			return f.Name(), f.Type(), true
		}
	}
	// fallback: case-insensitive field-name match (genqlient exports e.g. Source_address)
	for i := 0; i < s.NumFields(); i++ {
		f := s.Field(i)
		if strings.EqualFold(f.Name(), gqlName) {
			return f.Name(), f.Type(), true
		}
	}
	return "", nil, false
}

// scalarAccess: goType is a wrapper struct with a Value field; return tf type + ".Value".
func (c *pkgCtx) scalarAccess(t types.Type) (string, string, error) {
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return "", "", fmt.Errorf("scalar wrapper is not a struct: %s", t)
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if strings.EqualFold(f.Name(), "value") {
			return tfTypeForGo(f.Type()), "." + f.Name(), nil
		}
	}
	return "", "", fmt.Errorf("scalar wrapper %s has no Value field", t)
}

func tfTypeForGo(t types.Type) string {
	b, ok := t.Underlying().(*types.Basic)
	if !ok {
		return "types.String" // time.Time, etc -> stringify at emission
	}
	switch b.Kind() {
	case types.Bool:
		return "types.Bool"
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
		return "types.Int64"
	case types.Float32, types.Float64:
		return "types.Float64"
	default:
		return "types.String"
	}
}

// unwrapToStruct: if t (or t.Node) resolves to a struct, return it; follows a single `Node` field.
func (c *pkgCtx) unwrapToStruct(t types.Type) *types.Struct {
	st := t.Underlying().(*types.Struct)
	if f, _, ok := c.fieldByJSONOrName(st, "node"); ok {
		_ = f
		// follow Node
		for i := 0; i < st.NumFields(); i++ {
			if strings.EqualFold(st.Field(i).Name(), "node") {
				return st.Field(i).Type().Underlying().(*types.Struct)
			}
		}
	}
	return st
}

// edgesNodeStruct: from a Paginated type, return the Node element struct (non-union).
func (c *pkgCtx) edgesNodeStruct(t types.Type) *types.Struct {
	nt := c.edgesNodeType(t)
	return nt.Underlying().(*types.Struct)
}

// edgesNodeType: Paginated -> Edges []Edged -> Node <T>; return T.
func (c *pkgCtx) edgesNodeType(t types.Type) types.Type {
	pag := t.Underlying().(*types.Struct)
	var edgesSlice *types.Slice
	for i := 0; i < pag.NumFields(); i++ {
		if strings.EqualFold(pag.Field(i).Name(), "edges") {
			edgesSlice = pag.Field(i).Type().Underlying().(*types.Slice)
		}
	}
	edged := edgesSlice.Elem().Underlying().(*types.Struct)
	for i := 0; i < edged.NumFields(); i++ {
		if strings.EqualFold(edged.Field(i).Name(), "node") {
			return edged.Field(i).Type()
		}
	}
	panic("edges element has no Node field")
}

func (c *pkgCtx) edgesNodeInterface(t types.Type) *types.Interface {
	return c.edgesNodeType(t).Underlying().(*types.Interface)
}

func (c *pkgCtx) edgesNodeNamed(t types.Type) *types.Named {
	return c.edgesNodeType(t).(*types.Named)
}

// findImplementer scans the package for a named type implementing iface whose
// name ends with "Node"+typeCond (genqlient's concrete naming convention).
func (c *pkgCtx) findImplementer(iface *types.Interface, typeCond string) *types.Named {
	suffix := "Node" + typeCond
	scope := c.pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		tn, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}
		named, ok := tn.Type().(*types.Named)
		if !ok {
			continue
		}
		if !strings.HasSuffix(named.Obj().Name(), suffix) {
			continue
		}
		if types.Implements(types.NewPointer(named), iface) || types.Implements(named, iface) {
			return named
		}
	}
	return nil
}
```

And a tiny tag helper (avoid importing reflect's StructTag semantics by hand — reuse it):
```go
import "reflect"

func reflectStructTag(tag string) reflect.StructTag { return reflect.StructTag(tag) }
```
(Place the `reflect` import with the others; shown separately for clarity.)

- [ ] **Step 2: Write the failing test (against the real SDK)**

`provider-generator/pkg/sdkintrospect/sdkintrospect_test.go`:
```go
package sdkintrospect

import (
	"os"
	"testing"

	"github.com/marcom4rtinez/infrahub-terraform-provider-generator/pkg/queryir"
)

func TestLoadFirewall(t *testing.T) {
	src, err := os.ReadFile("../queryir/testdata/firewall.gql")
	if err != nil {
		t.Fatal(err)
	}
	q, err := queryir.Parse(string(src))
	if err != nil {
		t.Fatal(err)
	}
	// The provider repo's sdk/ dir, relative to this test package.
	res, err := Load("../../../sdk", q)
	if err != nil {
		t.Fatal(err)
	}
	if res.SDKFunc != "FirewallPolicyRules" {
		t.Errorf("SDKFunc = %q, want FirewallPolicyRules", res.SDKFunc)
	}
	// Find the source_address union and assert both concrete Go types resolved.
	var sa *RNode
	for _, c := range res.Root.Children {
		if c.IR.GqlField == "source_address" {
			sa = c
		}
	}
	if sa == nil || len(sa.Variants) != 2 {
		t.Fatalf("source_address variants unresolved: %#v", sa)
	}
	for _, v := range sa.Variants {
		if v.GoConcrete == "" {
			t.Errorf("variant %s has empty GoConcrete", v.TypeCond)
		}
		if len(v.Children) == 0 {
			t.Errorf("variant %s resolved no fields", v.TypeCond)
		}
	}
}
```

- [ ] **Step 3: Run the test**

Run: `cd provider-generator && go mod tidy && go test ./pkg/sdkintrospect/ -run TestLoadFirewall -v && cd ..`
Expected: PASS — `SDKFunc=FirewallPolicyRules`, two variants (`SecurityIPAddress`, `SecurityPrefix`) each with non-empty `GoConcrete` and ≥2 resolved fields (`name`, `address`/`prefix`).

> Note: the test depends on `../../../sdk` being generated. It is (the repo already ran genqlient). If a reviewer wipes it, run `make generate_sdk` first. This coupling is acceptable for a build-tooling test.

- [ ] **Step 4: Commit**

```bash
git add provider-generator/pkg/sdkintrospect
git commit -m "feat(gen): go/types SDK introspection resolving unions to concrete types"
```

---

### Task 3: Recursive emitter (schema + model + Read)

Emit one Terraform model struct per List/Object/Union level. Unions flatten to a single struct holding `typename` plus the union of all variant fields (each nullable); `Read` fills it with a `switch`. Lists become `[]Model` + `ListNestedAttribute`; scalars become leaf attributes typed from `TFType`.

**Files:**
- Create: `provider-generator/pkg/emit/datasource.go`
- Test: `provider-generator/pkg/emit/datasource_test.go`

- [ ] **Step 1: Implement the emitter**

`provider-generator/pkg/emit/datasource.go` — build the Go source as nested structs + a recursive `Read`. The emitter walks `*sdkintrospect.RNode`. Key rules:

- **Naming:** model struct names = `<BaseName>` + Title-cased path (e.g. `firewallPolicyRulesModel`, `firewallPolicyRulesSourceAddressModel`). tfsdk tags = `RNode.IR.TFName`. Field names = Title-cased `TFName`.
- **Scalar field:** `Name types.String `tfsdk:"name"`` ; assigned `types.StringValue(parent.Name.Value)` (or `types.Int64Value(int64(...))` / `types.BoolValue(...)` per `TFType`).
- **Object field:** a nested `*ChildModel` (SingleNestedAttribute) assigned by a local block reading `parent.<GoField>.Node...`.
- **List field:** `[]ChildModel` + `ListNestedAttribute`; assigned via `for _, e := range parent.<GoField>.Edges { ... append ... }` reading `e.Node.<...>`.
- **Union field:** `[]ChildModel` + `ListNestedAttribute`; the child model has `Typename types.String` + every variant field (deduped by TFName). Assigned via `for _, e := range parent.<GoField>.Edges { switch n := e.Node.(type) { case *<GoConcrete>: m.Address = types.StringValue(n.Address.Value); ... } }`. Fields not present on the matched concrete type stay null.

Provide:
```go
package emit

import (
	"bytes"
	"fmt"
	"go/format"

	"github.com/marcom4rtinez/infrahub-terraform-provider-generator/pkg/sdkintrospect"
)

// DataSource renders the full *_data_source.go file source.
func DataSource(res *sdkintrospect.Resolved) (string, error) {
	var b bytes.Buffer
	// ... write package/imports, struct registry (collected via a pre-walk),
	//     Metadata, Schema (recursive attribute tree), Read (recursive mapping),
	//     Configure (static, copied from artifact_data_source.go) ...
	src := b.String()
	formatted, err := format.Source([]byte(src))
	if err != nil {
		return src, fmt.Errorf("emitted source did not gofmt: %w\n%s", err, src)
	}
	return string(formatted), nil
}
```

Implementation detail: do a **pre-walk** to assign each List/Object/Union node a unique Go model type name and collect them, then render (1) all model structs, (2) the schema attribute tree, (3) the Read mapping, each by recursing the same tree. Use `go/format.Source` as a built-in correctness check — if the emitted text is not valid Go, the emitter fails loudly in tests rather than producing the upstream's garbage.

> The exact rendering is built test-first in Step 2–3: the gate is "emitted source parses and gofmts, and the whole provider compiles." Write the recursion to satisfy that gate; `go/format.Source` + `go build ./...` are unambiguous.

- [ ] **Step 2: Write the failing test**

`provider-generator/pkg/emit/datasource_test.go`:
```go
package emit

import (
	"os"
	"strings"
	"testing"

	"github.com/marcom4rtinez/infrahub-terraform-provider-generator/pkg/queryir"
	"github.com/marcom4rtinez/infrahub-terraform-provider-generator/pkg/sdkintrospect"
)

func TestEmitFirewall(t *testing.T) {
	src, _ := os.ReadFile("../queryir/testdata/firewall.gql")
	q, err := queryir.Parse(string(src))
	if err != nil {
		t.Fatal(err)
	}
	res, err := sdkintrospect.Load("../../../sdk", q)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DataSource(res)
	if err != nil {
		t.Fatalf("emit failed (means invalid Go was generated): %v", err)
	}
	// Must call the real SDK function name, not the mis-cased one.
	if !strings.Contains(out, "infrahub_sdk.FirewallPolicyRules(") {
		t.Error("did not call infrahub_sdk.FirewallPolicyRules")
	}
	// Must type-switch on a concrete address type and read its concrete field.
	if !strings.Contains(out, "case *infrahub_sdk.") {
		t.Error("no type-switch over concrete genqlient types")
	}
	if !strings.Contains(out, "ListNestedAttribute") {
		t.Error("no nested list attributes emitted")
	}
	// No literal '...' placeholders (the upstream bug).
	if strings.Contains(out, "Node...") || strings.Contains(out, "node_...") {
		t.Error("emitted the upstream '...' placeholder bug")
	}
}
```

- [ ] **Step 3: Iterate emitter until the test passes**

Run: `cd provider-generator && go test ./pkg/emit/ -run TestEmitFirewall -v && cd ..`
Expected: PASS. If `DataSource` returns the gofmt error, read the embedded source it returns and fix the recursion.

- [ ] **Step 4: Commit**

```bash
git add provider-generator/pkg/emit
git commit -m "feat(gen): recursive schema/model/Read emitter with union type-switches"
```

---

### Task 4: Route the data-source path through the new pipeline

Keep resources on the legacy parser. Only the **data-source** branch changes.

**Files:**
- Modify: `provider-generator/cmd/generator/main.go` (add `-sdk-dir` flag, thread it down)
- Modify: `provider-generator/pkg/parser/generators.go:38-67` (data-source branch)

- [ ] **Step 1: Add the `-sdk-dir` flag**

In `main.go`, alongside the existing flags:
```go
sdkDir := flag.String("sdk-dir", "sdk", "Directory of the genqlient-generated SDK module")
```
Thread `*sdkDir` into `parser.ReadAndGenerateDataSourcesAndResources(string(data), *providerDirectory, *sdkDir)`.

- [ ] **Step 2: Switch the data-source branch to the new pipeline**

In `generators.go`, change `ReadAndGenerateDataSourcesAndResources` to accept `sdkDir string`, and replace the `if parsedQuery.ResourceType == DataSource { ... }` body with:
```go
if parsedQuery.ResourceType == DataSource {
	q, err := queryir.Parse(graphqlQuery)
	if err != nil {
		fmt.Println("Error parsing GraphQL query:", err)
		os.Exit(1)
	}
	res, err := sdkintrospect.Load(sdkDir, q)
	if err != nil {
		fmt.Println("Error introspecting SDK:", err)
		os.Exit(1)
	}
	code, err := emit.DataSource(res)
	if err != nil {
		fmt.Println("Error emitting data source:", err)
		os.Exit(1)
	}
	file, err := os.Create(fmt.Sprintf("%s/%s_data_source.go", providerDirectory, q.BaseName))
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	if _, err = file.WriteString(code); err != nil {
		return "", "", err
	}
	fmt.Printf("Content written to %s_data_source.go file successfully!\n", q.BaseName)
	return q.BaseName, "", nil
}
```
Add imports for `queryir`, `sdkintrospect`, `emit`. Leave `parseGraphQLQuery`/the resource branch as-is (the legacy `parsedQuery` is still used to detect DataSource-vs-Resource via the existing `parseGraphQLQuery`).

> Detection note: `parseGraphQLQuery` already classifies query-vs-mutation by scanning for `query`/`mutation`. Keep using it solely for that branch decision; the data-source branch then re-parses with `queryir`.

- [ ] **Step 3: Build the fork**

Run: `cd provider-generator && go build ./... && cd ..`
Expected: builds clean.

- [ ] **Step 4: Commit**

```bash
git add provider-generator/cmd provider-generator/pkg/parser/generators.go
git commit -m "feat(gen): route data-source generation through AST+introspection pipeline"
```

---

### Task 5: End-to-end — regenerate and compile the real provider

**Files:**
- Regenerates: `internal/provider/firewallPolicyRules_data_source.go`, `internal/provider/provider.go`

- [ ] **Step 1: Regenerate the SDK then the provider**

```bash
make generate_sdk
cd provider-generator && go run ./cmd/generator --artifacts -gql-dir ../gql -provider-dir ../internal/provider -sdk-dir ../sdk && cd ..
```
Expected: "Content written to firewallPolicyRules_data_source.go file successfully!" with no `...` in the file.

- [ ] **Step 2: Inspect the generated data source**

Run: `gofmt -l internal/provider/firewallPolicyRules_data_source.go`
Expected: no output (already gofmt-clean — the emitter ran `format.Source`).

Run: `grep -c 'case \*infrahub_sdk' internal/provider/firewallPolicyRules_data_source.go`
Expected: ≥3 (SecurityIPAddress, SecurityPrefix, SecurityService).

- [ ] **Step 3: Compile the whole provider (the real acceptance gate)**

Run: `go build ./...`
Expected: exit 0, no errors.

- [ ] **Step 4: Run the original failing command end-to-end**

Run: `make all`
Expected: passes `fmt` and `build`/`install` (the steps that previously errored at `GNUmakefile:41`). `lint`/`generate` require `golangci-lint`/docs tooling per the blog's prerequisites; if those tools are absent locally, confirm `fmt`+`build`+`install` succeeded and note the tool gap.

- [ ] **Step 5: Commit the regenerated provider**

```bash
git add internal/provider/firewallPolicyRules_data_source.go internal/provider/provider.go sdk/generated_graphql_client.go
git commit -m "feat: regenerate firewall_policy_rules data source (compiles, exposes union fields)"
```

---

### Task 6: Update the blog to match reality

**Files:**
- Modify: `../infrahub-firewall-cicd/blog/part-2-build-the-provider.md`

- [ ] **Step 1: Correct the build-order paragraph and the generator reference**

Around lines 144-147, the post says `make all` runs `automatic_generator` then `generate_sdk`. Update to reflect the reorder (genqlient first, so the generator can introspect the SDK) and that the generator is a fork supporting inline fragments + lists. Around lines 60/127-129, point readers at the in-repo fork rather than `@latest`, and add a one-paragraph "Gotcha: the upstream generator can't read inline fragments / lists" callout matching the existing gotcha style.

- [ ] **Step 2: Commit**

```bash
cd ../infrahub-firewall-cicd && git add blog/part-2-build-the-provider.md && \
  git commit -m "docs: part-2 reflects forked generator (fragments+lists) and make reorder" && cd -
```

---

## Self-Review

- **Spec coverage:** inline fragments → Task 1 (Union IR) + Task 2 (variant resolution) + Task 3 (type-switch emit) ✓; nested lists → Task 1 (List) + Task 3 (ListNestedAttribute/for-loops) ✓; function-name casing → Task 2 (`SDKFunc` from real symbol) + Task 3 test asserts `FirewallPolicyRules(` ✓; "make all works" → Task 0 (reorder/wiring) + Task 5 (compile gate) ✓; blog accuracy → Task 6 ✓.
- **Placeholder scan:** emitter rendering in Task 3 is intentionally test-driven against the `go/format` + `go build` gates rather than transcribed line-by-line; every other step has concrete code/commands. The two gates make "done" unambiguous.
- **Type consistency:** `queryir.Query/Node/Variant` (Task 1) are consumed unchanged by `sdkintrospect.Load/RNode/RVariant` (Task 2); `emit.DataSource(*sdkintrospect.Resolved)` (Task 3) matches the call in `generators.go` (Task 4); `-sdk-dir` flag (Task 4) matches the Makefile invocation (Task 0) and test paths (`../../../sdk`).
- **Resource path:** untouched — only the DataSource branch of `generators.go` changes; `parseResourceInput` and the resource template remain.
