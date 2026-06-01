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
	GqlField string // graphql field name on its parent ("source_address", "name", "action")
	TFName   string // terraform attribute name (defaults to GqlField)
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

	if onlyValueLeaf(ss) {
		n.Kind = Scalar
		return n
	}
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
	if node := childByName(ss, "node"); node != nil {
		n.Kind = Object
		n.Children = buildChildren(node.SelectionSet)
		return n
	}
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
