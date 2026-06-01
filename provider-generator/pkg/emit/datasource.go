// Package emit turns a resolved query (queryir + SDK introspection) into the
// source of a terraform-plugin-framework data source .go file.
package emit

import (
	"bytes"
	"fmt"
	"go/format"
	"strings"

	"github.com/marcom4rtinez/infrahub-terraform-provider-generator/pkg/queryir"
	"github.com/marcom4rtinez/infrahub-terraform-provider-generator/pkg/sdkintrospect"
)

// modelType holds a pre-walked model struct type assigned to a List/Object/Union node.
type modelType struct {
	name string // Go type name, e.g. "firewallPolicyRulesSourceAddressModel"
	node *sdkintrospect.RNode
}

// emitter carries shared state while rendering.
type emitter struct {
	res       *sdkintrospect.Resolved
	models    []modelType                     // in declaration order
	modelName map[*sdkintrospect.RNode]string // node -> model type name
	varSeq    int                             // monotonic counter for unique local vars in Read
}

// DataSource returns the full source of a *_data_source.go file (package
// provider), gofmt'd. If formatting fails it returns the unformatted source
// alongside the error so the bad code is visible.
func DataSource(res *sdkintrospect.Resolved) (string, error) {
	e := &emitter{
		res:       res,
		modelName: map[*sdkintrospect.RNode]string{},
	}
	// Pre-walk: assign a unique model type name to each List/Object/Union node.
	e.assignModels(res.Root, "")

	var b bytes.Buffer
	e.renderHeader(&b)
	e.renderModels(&b)
	e.renderMetadata(&b)
	e.renderSchema(&b)
	e.renderRead(&b)
	e.renderConfigure(&b)

	out := b.String()
	formatted, err := format.Source([]byte(out))
	if err != nil {
		return out, fmt.Errorf("gofmt failed: %w", err)
	}
	return string(formatted), nil
}

// ---------------------------------------------------------------------------
// pre-walk: model type assignment
// ---------------------------------------------------------------------------

func (e *emitter) baseTitle() string {
	return toGoName(e.res.Query.BaseName)
}

// assignModels walks the tree assigning a model type name to each
// List/Object/Union node. path is the accumulated PascalCase path suffix.
func (e *emitter) assignModels(n *sdkintrospect.RNode, path string) {
	switch n.IR.Kind {
	case queryir.List, queryir.Object, queryir.Union:
		name := e.res.Query.BaseName + path + "Model"
		e.models = append(e.models, modelType{name: name, node: n})
		e.modelName[n] = name
		for _, c := range n.Children {
			e.assignModels(c, path+toGoName(c.IR.TFName))
		}
		for _, v := range n.Variants {
			for _, c := range v.Children {
				e.assignModels(c, path+toGoName(c.IR.TFName))
			}
		}
	}
}

// ---------------------------------------------------------------------------
// header / imports / constructor / top struct
// ---------------------------------------------------------------------------

func (e *emitter) renderHeader(b *bytes.Buffer) {
	base := e.baseTitle()           // FirewallPolicyRules
	baseLower := e.res.Query.BaseName // firewallPolicyRules
	rootElem := e.modelName[e.res.Root]

	b.WriteString("package provider\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"context\"\n")
	b.WriteString("\t\"fmt\"\n\n")
	b.WriteString("\tinfrahub_sdk \"github.com/opsmill/infrahub-sdk-go\"\n\n")
	b.WriteString("\t\"github.com/Khan/genqlient/graphql\"\n")
	b.WriteString("\t\"github.com/hashicorp/terraform-plugin-framework/datasource\"\n")
	b.WriteString("\t\"github.com/hashicorp/terraform-plugin-framework/datasource/schema\"\n")
	b.WriteString("\t\"github.com/hashicorp/terraform-plugin-framework/types\"\n")
	b.WriteString("\t\"github.com/hashicorp/terraform-plugin-log/tflog\"\n")
	b.WriteString(")\n\n")

	dsType := baseLower + "DataSource"
	b.WriteString("// Ensure the implementation satisfies the expected interfaces.\n")
	b.WriteString("var (\n")
	fmt.Fprintf(b, "\t_ datasource.DataSource              = &%s{}\n", dsType)
	fmt.Fprintf(b, "\t_ datasource.DataSourceWithConfigure = &%s{}\n", dsType)
	b.WriteString(")\n\n")

	fmt.Fprintf(b, "// New%sDataSource is a helper function to simplify the provider implementation.\n", base)
	fmt.Fprintf(b, "func New%sDataSource() datasource.DataSource {\n", base)
	fmt.Fprintf(b, "\treturn &%s{}\n}\n\n", dsType)

	// Top data source struct.
	fmt.Fprintf(b, "// %s is the data source implementation.\n", dsType)
	fmt.Fprintf(b, "type %s struct {\n", dsType)
	b.WriteString("\tclient *graphql.Client\n")
	if e.res.Query.VarName != "" {
		fmt.Fprintf(b, "\t%s types.String `tfsdk:%q`\n", toGoName(e.res.Query.VarName), toSnake(e.res.Query.VarName))
	}
	fmt.Fprintf(b, "\t%s []%s `tfsdk:%q`\n", toGoName(e.res.Query.RootObject), rootElem, toSnake(e.res.Query.RootObject))
	b.WriteString("}\n\n")
}

// ---------------------------------------------------------------------------
// model structs
// ---------------------------------------------------------------------------

func (e *emitter) renderModels(b *bytes.Buffer) {
	for _, m := range e.models {
		fmt.Fprintf(b, "type %s struct {\n", m.name)
		if m.node.IR.Kind == queryir.Union {
			b.WriteString("\tTypename types.String `tfsdk:\"typename\"`\n")
			for _, c := range e.dedupUnionChildren(m.node) {
				e.renderModelField(b, c)
			}
		} else {
			for _, c := range m.node.Children {
				e.renderModelField(b, c)
			}
		}
		b.WriteString("}\n\n")
	}
}

func (e *emitter) renderModelField(b *bytes.Buffer, c *sdkintrospect.RNode) {
	tag := toSnake(c.IR.TFName)
	field := toGoName(c.IR.TFName)
	switch c.IR.Kind {
	case queryir.Scalar:
		fmt.Fprintf(b, "\t%s %s `tfsdk:%q`\n", field, c.TFType, tag)
	case queryir.Object:
		fmt.Fprintf(b, "\t%s %s `tfsdk:%q`\n", field, e.modelName[c], tag)
	case queryir.List, queryir.Union:
		fmt.Fprintf(b, "\t%s []%s `tfsdk:%q`\n", field, e.modelName[c], tag)
	}
}

// dedupUnionChildren returns the deduped union of all variant children of a
// Union node, keyed by TFName (first occurrence wins).
func (e *emitter) dedupUnionChildren(n *sdkintrospect.RNode) []*sdkintrospect.RNode {
	seen := map[string]bool{}
	var out []*sdkintrospect.RNode
	for _, v := range n.Variants {
		for _, c := range v.Children {
			key := toSnake(c.IR.TFName)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, c)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

func (e *emitter) renderMetadata(b *bytes.Buffer) {
	dsType := e.res.Query.BaseName + "DataSource"
	fmt.Fprintf(b, "// Metadata returns the data source type name.\n")
	fmt.Fprintf(b, "func (d *%s) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {\n", dsType)
	fmt.Fprintf(b, "\tresp.TypeName = req.ProviderTypeName + %q\n", "_"+e.res.Query.OpName)
	b.WriteString("}\n\n")
}

// ---------------------------------------------------------------------------
// Schema
// ---------------------------------------------------------------------------

func (e *emitter) renderSchema(b *bytes.Buffer) {
	dsType := e.res.Query.BaseName + "DataSource"
	fmt.Fprintf(b, "// Schema defines the schema for the data source.\n")
	fmt.Fprintf(b, "func (d *%s) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {\n", dsType)
	b.WriteString("\tresp.Schema = schema.Schema{\n")
	b.WriteString("\t\tAttributes: map[string]schema.Attribute{\n")
	if e.res.Query.VarName != "" {
		fmt.Fprintf(b, "\t\t\t%q: schema.StringAttribute{\n\t\t\t\tRequired: true,\n\t\t\t},\n", toSnake(e.res.Query.VarName))
	}
	// Root object -> list nested attribute.
	fmt.Fprintf(b, "\t\t\t%q: ", toSnake(e.res.Query.RootObject))
	e.renderSchemaAttr(b, e.res.Root, "\t\t\t")
	b.WriteString(",\n")
	b.WriteString("\t\t},\n")
	b.WriteString("\t}\n}\n\n")
}

// renderSchemaAttr writes the schema.Attribute expression for node n at the
// given indent (no trailing comma/newline).
func (e *emitter) renderSchemaAttr(b *bytes.Buffer, n *sdkintrospect.RNode, indent string) {
	switch n.IR.Kind {
	case queryir.Scalar:
		switch n.TFType {
		case "types.Int64":
			b.WriteString("schema.Int64Attribute{Computed: true}")
		case "types.Bool":
			b.WriteString("schema.BoolAttribute{Computed: true}")
		case "types.Float64":
			b.WriteString("schema.Float64Attribute{Computed: true}")
		default:
			b.WriteString("schema.StringAttribute{Computed: true}")
		}
	case queryir.Object:
		b.WriteString("schema.SingleNestedAttribute{\n")
		fmt.Fprintf(b, "%s\tComputed: true,\n", indent)
		fmt.Fprintf(b, "%s\tAttributes: map[string]schema.Attribute{\n", indent)
		for _, c := range n.Children {
			e.renderSchemaChild(b, c, indent+"\t\t")
		}
		fmt.Fprintf(b, "%s\t},\n", indent)
		fmt.Fprintf(b, "%s}", indent)
	case queryir.List:
		b.WriteString("schema.ListNestedAttribute{\n")
		fmt.Fprintf(b, "%s\tComputed: true,\n", indent)
		fmt.Fprintf(b, "%s\tNestedObject: schema.NestedAttributeObject{\n", indent)
		fmt.Fprintf(b, "%s\t\tAttributes: map[string]schema.Attribute{\n", indent)
		for _, c := range n.Children {
			e.renderSchemaChild(b, c, indent+"\t\t\t")
		}
		fmt.Fprintf(b, "%s\t\t},\n", indent)
		fmt.Fprintf(b, "%s\t},\n", indent)
		fmt.Fprintf(b, "%s}", indent)
	case queryir.Union:
		b.WriteString("schema.ListNestedAttribute{\n")
		fmt.Fprintf(b, "%s\tComputed: true,\n", indent)
		fmt.Fprintf(b, "%s\tNestedObject: schema.NestedAttributeObject{\n", indent)
		fmt.Fprintf(b, "%s\t\tAttributes: map[string]schema.Attribute{\n", indent)
		fmt.Fprintf(b, "%s\t\t\t%q: schema.StringAttribute{Computed: true},\n", indent, "typename")
		for _, c := range e.dedupUnionChildren(n) {
			e.renderSchemaChild(b, c, indent+"\t\t\t")
		}
		fmt.Fprintf(b, "%s\t\t},\n", indent)
		fmt.Fprintf(b, "%s\t},\n", indent)
		fmt.Fprintf(b, "%s}", indent)
	}
}

func (e *emitter) renderSchemaChild(b *bytes.Buffer, c *sdkintrospect.RNode, indent string) {
	fmt.Fprintf(b, "%s%q: ", indent, toSnake(c.IR.TFName))
	e.renderSchemaAttr(b, c, indent)
	b.WriteString(",\n")
}

// ---------------------------------------------------------------------------
// Read
// ---------------------------------------------------------------------------

func (e *emitter) renderRead(b *bytes.Buffer) {
	dsType := e.res.Query.BaseName + "DataSource"
	fmt.Fprintf(b, "func (d *%s) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {\n", dsType)
	fmt.Fprintf(b, "\ttflog.Info(ctx, \"Reading %s...\\n\")\n", e.res.Query.OpName)
	fmt.Fprintf(b, "\tvar config %s\n\n", dsType)
	b.WriteString("\tdiags := req.Config.Get(ctx, &config)\n")
	b.WriteString("\tresp.Diagnostics.Append(diags...)\n")
	b.WriteString("\tif resp.Diagnostics.HasError() {\n\t\treturn\n\t}\n\n")

	// SDK call.
	if e.res.Query.VarName != "" {
		fmt.Fprintf(b, "\tresponse, err := infrahub_sdk.%s(ctx, *d.client, config.%s.ValueString())\n",
			e.res.SDKFunc, toGoName(e.res.Query.VarName))
	} else {
		fmt.Fprintf(b, "\tresponse, err := infrahub_sdk.%s(ctx, *d.client)\n", e.res.SDKFunc)
	}
	b.WriteString("\tif err != nil {\n")
	b.WriteString("\t\tresp.Diagnostics.AddError(\n")
	b.WriteString("\t\t\t\"Unable to read data from Infrahub\",\n")
	b.WriteString("\t\t\terr.Error(),\n")
	b.WriteString("\t\t)\n\t\treturn\n\t}\n\n")

	fmt.Fprintf(b, "\tvar state %s\n", dsType)
	if e.res.Query.VarName != "" {
		fmt.Fprintf(b, "\tstate.%s = config.%s\n", toGoName(e.res.Query.VarName), toGoName(e.res.Query.VarName))
	}

	// Root is a List: loop over response.<GoField>.Edges.
	rootGo := "response." + e.res.Root.GoField
	stateField := "state." + toGoName(e.res.Query.RootObject)
	e.renderReadList(b, e.res.Root, rootGo, stateField, "\t")

	b.WriteString("\n\tdiags = resp.State.Set(ctx, &state)\n")
	b.WriteString("\tresp.Diagnostics.Append(diags...)\n")
	b.WriteString("\tif resp.Diagnostics.HasError() {\n\t\treturn\n\t}\n")
	b.WriteString("}\n\n")
}

// nextVar returns a process-unique local variable name with the given prefix.
func (e *emitter) nextVar(prefix string) string {
	e.varSeq++
	return fmt.Sprintf("%s%d", prefix, e.varSeq)
}

// renderReadList emits a `for _, eN := range <listExpr>.Edges` loop that builds
// an element model and appends it to dst (a slice field expression). n is a
// List or Union node.
func (e *emitter) renderReadList(b *bytes.Buffer, n *sdkintrospect.RNode, listExpr, dst, indent string) {
	ev := e.nextVar("e")
	mv := e.nextVar("m")
	model := e.modelName[n]

	fmt.Fprintf(b, "%sfor _, %s := range %s.Edges {\n", indent, ev, listExpr)
	in := indent + "\t"
	fmt.Fprintf(b, "%svar %s %s\n", in, mv, model)

	if n.IR.Kind == queryir.Union {
		nv := e.nextVar("n")
		fmt.Fprintf(b, "%sswitch %s := %s.Node.(type) {\n", in, nv, ev)
		for _, v := range n.Variants {
			fmt.Fprintf(b, "%scase *infrahub_sdk.%s:\n", in, v.GoConcrete)
			cin := in + "\t"
			fmt.Fprintf(b, "%s%s.Typename = types.StringValue(%s.GetTypename())\n", cin, mv, nv)
			for _, c := range v.Children {
				e.renderReadChild(b, c, nv, mv, cin)
			}
		}
		fmt.Fprintf(b, "%s}\n", in)
	} else {
		// Plain list: element value is eN.Node.
		nodeExpr := ev + ".Node"
		for _, c := range n.Children {
			e.renderReadChild(b, c, nodeExpr, mv, in)
		}
	}

	fmt.Fprintf(b, "%s%s = append(%s, %s)\n", in, dst, dst, mv)
	fmt.Fprintf(b, "%s}\n", indent)
}

// renderReadChild maps a child node c whose parent value expression is `parent`
// into the model variable `mv` (mv.<Field>).
func (e *emitter) renderReadChild(b *bytes.Buffer, c *sdkintrospect.RNode, parent, mv, indent string) {
	field := mv + "." + toGoName(c.IR.TFName)
	switch c.IR.Kind {
	case queryir.Scalar:
		access := parent + "." + c.GoField + c.Access
		fmt.Fprintf(b, "%s%s = %s\n", indent, field, scalarValue(c.TFType, access))
	case queryir.Object:
		// Recurse into a nested model value. The element value for children is
		// parent.<GoField><Unwrap> (Unwrap is ".Node" when the introspector
		// followed a node field).
		childParent := parent + "." + c.GoField + c.Unwrap
		nv := e.nextVar("o")
		fmt.Fprintf(b, "%svar %s %s\n", indent, nv, e.modelName[c])
		for _, gc := range c.Children {
			e.renderReadChild(b, gc, childParent, nv, indent)
		}
		fmt.Fprintf(b, "%s%s = %s\n", indent, field, nv)
	case queryir.List, queryir.Union:
		listExpr := parent + "." + c.GoField
		e.renderReadList(b, c, listExpr, field, indent)
	}
}

func scalarValue(tfType, access string) string {
	switch tfType {
	case "types.Int64":
		return fmt.Sprintf("types.Int64Value(int64(%s))", access)
	case "types.Bool":
		return fmt.Sprintf("types.BoolValue(%s)", access)
	case "types.Float64":
		return fmt.Sprintf("types.Float64Value(float64(%s))", access)
	default:
		return fmt.Sprintf("types.StringValue(%s)", access)
	}
}

// ---------------------------------------------------------------------------
// Configure (copied verbatim from artifact_data_source.go)
// ---------------------------------------------------------------------------

func (e *emitter) renderConfigure(b *bytes.Buffer) {
	dsType := e.res.Query.BaseName + "DataSource"
	fmt.Fprintf(b, "// Configure adds the provider configured client to the data source.\n")
	fmt.Fprintf(b, "func (d *%s) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {\n", dsType)
	b.WriteString("\t// Add a nil check when handling ProviderData because Terraform\n")
	b.WriteString("\t// sets that data after it calls the ConfigureProvider RPC.\n")
	b.WriteString("\tif req.ProviderData == nil {\n\t\treturn\n\t}\n\n")
	b.WriteString("\tclient, ok := req.ProviderData.(graphql.Client)\n")
	b.WriteString("\tif !ok {\n")
	b.WriteString("\t\tresp.Diagnostics.AddError(\n")
	b.WriteString("\t\t\t\"Unexpected Data Source Configure Type\",\n")
	b.WriteString("\t\t\tfmt.Sprintf(\"Expected *graphql.Client, got: %T. Please report this issue to the provider developers.\", req.ProviderData),\n")
	b.WriteString("\t\t)\n\n\t\treturn\n\t}\n\n")
	b.WriteString("\td.client = &client\n")
	b.WriteString("}\n")
}

// ---------------------------------------------------------------------------
// naming helpers
// ---------------------------------------------------------------------------

// toSnake converts PascalCase/camelCase to snake_case. Already-snake/lowercase
// strings pass through unchanged.
func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 && s[i-1] != '_' {
				b.WriteByte('_')
			}
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// toGoName converts a snake_case (or arbitrary) name to a Go-exported-style
// PascalCase identifier (e.g. source_address -> SourceAddress).
func toGoName(s string) string {
	parts := strings.Split(s, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	return b.String()
}
