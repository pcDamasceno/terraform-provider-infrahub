package sdkintrospect

import (
	"errors"
	"fmt"
	"go/types"
	"reflect"
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
	TFType   string   // for Scalar: "types.String"|"types.Int64"|"types.Bool"|"types.Float64"
	Access   string   // for Scalar: accessor suffix from the object value, e.g. ".Value"
	Children []*RNode // Object/List members
	Variants []*RVariant
}

type RVariant struct {
	TypeCond   string   // graphql concrete type, e.g. "SecurityIPAddress"
	GoConcrete string   // concrete genqlient struct name (no pointer), used in `case *X:`
	Children   []*RNode // selected fields resolved against the concrete struct
}

type pkgCtx struct {
	pkg *types.Package
}

func Load(sdkDir string, q *queryir.Query) (*Resolved, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedDeps | packages.NeedImports | packages.NeedTypesInfo,
		Dir:  sdkDir,
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 || pkgs[0].Types == nil {
		return nil, fmt.Errorf("could not load SDK package from %s", sdkDir)
	}
	if len(pkgs[0].Errors) > 0 {
		var errs []error
		for _, e := range pkgs[0].Errors {
			errs = append(errs, fmt.Errorf("%s", e))
		}
		return nil, fmt.Errorf("loading SDK package from %s: %w", sdkDir, errors.Join(errs...))
	}
	ctx := &pkgCtx{pkg: pkgs[0].Types}

	fn := ctx.pkg.Scope().Lookup(q.OpName)
	if fn == nil {
		return nil, fmt.Errorf("SDK has no exported %s function", q.OpName)
	}
	sig := fn.Type().(*types.Signature)
	respPtr := sig.Results().At(0).Type().(*types.Pointer)
	respStruct := respPtr.Elem().Underlying().(*types.Struct)

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

func (c *pkgCtx) resolveNode(n *queryir.Node, goField string, goType types.Type) (*RNode, error) {
	r := &RNode{IR: n, GoField: goField}
	switch n.Kind {
	case queryir.Scalar:
		tf, access, err := c.scalarAccess(goType)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n.GqlField, err)
		}
		r.TFType, r.Access = tf, access
		return r, nil

	case queryir.Object:
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
		iface := c.edgesNodeInterface(goType)
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

func (c *pkgCtx) fieldByJSONOrName(s *types.Struct, gqlName string) (string, types.Type, bool) {
	for i := 0; i < s.NumFields(); i++ {
		f := s.Field(i)
		tag := reflect.StructTag(s.Tag(i)).Get("json")
		if name := strings.Split(tag, ",")[0]; name == gqlName {
			return f.Name(), f.Type(), true
		}
	}
	for i := 0; i < s.NumFields(); i++ {
		f := s.Field(i)
		if strings.EqualFold(f.Name(), gqlName) {
			return f.Name(), f.Type(), true
		}
	}
	return "", nil, false
}

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
		return "types.String"
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

func (c *pkgCtx) unwrapToStruct(t types.Type) *types.Struct {
	st := t.Underlying().(*types.Struct)
	for i := 0; i < st.NumFields(); i++ {
		if strings.EqualFold(st.Field(i).Name(), "node") {
			return st.Field(i).Type().Underlying().(*types.Struct)
		}
	}
	return st
}

func (c *pkgCtx) edgesNodeStruct(t types.Type) *types.Struct {
	return c.edgesNodeType(t).Underlying().(*types.Struct)
}

func (c *pkgCtx) edgesNodeType(t types.Type) types.Type {
	pag := t.Underlying().(*types.Struct)
	var edgesSlice *types.Slice
	for i := 0; i < pag.NumFields(); i++ {
		if strings.EqualFold(pag.Field(i).Name(), "edges") {
			edgesSlice = pag.Field(i).Type().Underlying().(*types.Slice)
		}
	}
	if edgesSlice == nil {
		panic(fmt.Sprintf("no Edges field on %s", t))
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
