package provider

import (
	"context"
	"fmt"

	infrahub_sdk "github.com/opsmill/infrahub-sdk-go"

	"github.com/Khan/genqlient/graphql"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ datasource.DataSource              = &firewallPolicyRulesDataSource{}
	_ datasource.DataSourceWithConfigure = &firewallPolicyRulesDataSource{}
)

// NewFirewallpolicyrulesDataSource is a helper function to simplify the provider implementation.
func NewFirewallpolicyrulesDataSource() datasource.DataSource {
	return &firewallPolicyRulesDataSource{}
}

// firewallPolicyRulesDataSource is the data source implementation.
type firewallPolicyRulesDataSource struct {
	client             *graphql.Client
	Policy             types.String               `tfsdk:"policy"`
	SecurityPolicyRule []firewallPolicyRulesModel `tfsdk:"security_policy_rule"`
}

type firewallPolicyRulesModel struct {
	Name                types.String                                  `tfsdk:"name"`
	Index               types.String                                  `tfsdk:"index"`
	Action              types.String                                  `tfsdk:"action"`
	Log                 types.Bool                                    `tfsdk:"log"`
	SourceZone          firewallPolicyRulesSourceZoneModel            `tfsdk:"source_zone"`
	DestinationZone     firewallPolicyRulesDestinationZoneModel       `tfsdk:"destination_zone"`
	SourceAddress       []firewallPolicyRulesSourceAddressModel       `tfsdk:"source_address"`
	DestinationAddress  []firewallPolicyRulesDestinationAddressModel  `tfsdk:"destination_address"`
	SourceServices      []firewallPolicyRulesSourceServicesModel      `tfsdk:"source_services"`
	DestinationServices []firewallPolicyRulesDestinationServicesModel `tfsdk:"destination_services"`
}

type firewallPolicyRulesSourceZoneModel struct {
	Name types.String `tfsdk:"name"`
}

type firewallPolicyRulesDestinationZoneModel struct {
	Name types.String `tfsdk:"name"`
}

type firewallPolicyRulesSourceAddressModel struct {
	Typename types.String `tfsdk:"typename"`
	Name     types.String `tfsdk:"name"`
	Address  types.String `tfsdk:"address"`
	Prefix   types.String `tfsdk:"prefix"`
}

type firewallPolicyRulesDestinationAddressModel struct {
	Typename types.String `tfsdk:"typename"`
	Name     types.String `tfsdk:"name"`
	Address  types.String `tfsdk:"address"`
	Prefix   types.String `tfsdk:"prefix"`
}

type firewallPolicyRulesSourceServicesModel struct {
	Typename types.String `tfsdk:"typename"`
	Name     types.String `tfsdk:"name"`
	Port     types.String `tfsdk:"port"`
}

type firewallPolicyRulesDestinationServicesModel struct {
	Typename types.String `tfsdk:"typename"`
	Name     types.String `tfsdk:"name"`
	Port     types.String `tfsdk:"port"`
}

// Metadata returns the data source type name.
func (d *firewallPolicyRulesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_FirewallPolicyRules"
}

// Schema defines the schema for the data source.
func (d *firewallPolicyRulesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"policy": schema.StringAttribute{
				Required: true,
			},
			"security_policy_rule": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name":   schema.StringAttribute{Computed: true},
						"index":  schema.StringAttribute{Computed: true},
						"action": schema.StringAttribute{Computed: true},
						"log":    schema.BoolAttribute{Computed: true},
						"source_zone": schema.SingleNestedAttribute{
							Computed: true,
							Attributes: map[string]schema.Attribute{
								"name": schema.StringAttribute{Computed: true},
							},
						},
						"destination_zone": schema.SingleNestedAttribute{
							Computed: true,
							Attributes: map[string]schema.Attribute{
								"name": schema.StringAttribute{Computed: true},
							},
						},
						"source_address": schema.ListNestedAttribute{
							Computed: true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"typename": schema.StringAttribute{Computed: true},
									"name":     schema.StringAttribute{Computed: true},
									"address":  schema.StringAttribute{Computed: true},
									"prefix":   schema.StringAttribute{Computed: true},
								},
							},
						},
						"destination_address": schema.ListNestedAttribute{
							Computed: true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"typename": schema.StringAttribute{Computed: true},
									"name":     schema.StringAttribute{Computed: true},
									"address":  schema.StringAttribute{Computed: true},
									"prefix":   schema.StringAttribute{Computed: true},
								},
							},
						},
						"source_services": schema.ListNestedAttribute{
							Computed: true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"typename": schema.StringAttribute{Computed: true},
									"name":     schema.StringAttribute{Computed: true},
									"port":     schema.StringAttribute{Computed: true},
								},
							},
						},
						"destination_services": schema.ListNestedAttribute{
							Computed: true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"typename": schema.StringAttribute{Computed: true},
									"name":     schema.StringAttribute{Computed: true},
									"port":     schema.StringAttribute{Computed: true},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (d *firewallPolicyRulesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	tflog.Info(ctx, "Reading FirewallPolicyRules...\n")
	var config firewallPolicyRulesDataSource

	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	response, err := infrahub_sdk.FirewallPolicyRules(ctx, *d.client, config.Policy.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read data from Infrahub",
			err.Error(),
		)
		return
	}

	var state firewallPolicyRulesDataSource
	state.Policy = config.Policy
	for _, e1 := range response.SecurityPolicyRule.Edges {
		var m2 firewallPolicyRulesModel
		m2.Name = types.StringValue(e1.Node.Name.Value)
		m2.Index = types.StringValue(e1.Node.Index.Value)
		m2.Action = types.StringValue(e1.Node.Action.Value)
		m2.Log = types.BoolValue(e1.Node.Log.Value)
		var o3 firewallPolicyRulesSourceZoneModel
		o3.Name = types.StringValue(e1.Node.Source_zone.Node.Name.Value)
		m2.SourceZone = o3
		var o4 firewallPolicyRulesDestinationZoneModel
		o4.Name = types.StringValue(e1.Node.Destination_zone.Node.Name.Value)
		m2.DestinationZone = o4
		for _, e5 := range e1.Node.Source_address.Edges {
			var m6 firewallPolicyRulesSourceAddressModel
			switch n7 := e5.Node.(type) {
			case *infrahub_sdk.FirewallPolicyRulesSecurityPolicyRulePaginatedSecurityPolicyRuleEdgesEdgedSecurityPolicyRuleNodeSecurityPolicyRuleSource_addressNestedPaginatedSecurityGenericAddressEdgesNestedEdgedSecurityGenericAddressNodeSecurityIPAddress:
				m6.Typename = types.StringValue(n7.GetTypename())
				m6.Name = types.StringValue(n7.Name.Value)
				m6.Address = types.StringValue(n7.Address.Value)
			case *infrahub_sdk.FirewallPolicyRulesSecurityPolicyRulePaginatedSecurityPolicyRuleEdgesEdgedSecurityPolicyRuleNodeSecurityPolicyRuleSource_addressNestedPaginatedSecurityGenericAddressEdgesNestedEdgedSecurityGenericAddressNodeSecurityPrefix:
				m6.Typename = types.StringValue(n7.GetTypename())
				m6.Name = types.StringValue(n7.Name.Value)
				m6.Prefix = types.StringValue(n7.Prefix.Value)
			}
			m2.SourceAddress = append(m2.SourceAddress, m6)
		}
		for _, e8 := range e1.Node.Destination_address.Edges {
			var m9 firewallPolicyRulesDestinationAddressModel
			switch n10 := e8.Node.(type) {
			case *infrahub_sdk.FirewallPolicyRulesSecurityPolicyRulePaginatedSecurityPolicyRuleEdgesEdgedSecurityPolicyRuleNodeSecurityPolicyRuleDestination_addressNestedPaginatedSecurityGenericAddressEdgesNestedEdgedSecurityGenericAddressNodeSecurityIPAddress:
				m9.Typename = types.StringValue(n10.GetTypename())
				m9.Name = types.StringValue(n10.Name.Value)
				m9.Address = types.StringValue(n10.Address.Value)
			case *infrahub_sdk.FirewallPolicyRulesSecurityPolicyRulePaginatedSecurityPolicyRuleEdgesEdgedSecurityPolicyRuleNodeSecurityPolicyRuleDestination_addressNestedPaginatedSecurityGenericAddressEdgesNestedEdgedSecurityGenericAddressNodeSecurityPrefix:
				m9.Typename = types.StringValue(n10.GetTypename())
				m9.Name = types.StringValue(n10.Name.Value)
				m9.Prefix = types.StringValue(n10.Prefix.Value)
			}
			m2.DestinationAddress = append(m2.DestinationAddress, m9)
		}
		for _, e11 := range e1.Node.Source_services.Edges {
			var m12 firewallPolicyRulesSourceServicesModel
			switch n13 := e11.Node.(type) {
			case *infrahub_sdk.FirewallPolicyRulesSecurityPolicyRulePaginatedSecurityPolicyRuleEdgesEdgedSecurityPolicyRuleNodeSecurityPolicyRuleSource_servicesNestedPaginatedSecurityGenericServiceEdgesNestedEdgedSecurityGenericServiceNodeSecurityService:
				m12.Typename = types.StringValue(n13.GetTypename())
				m12.Name = types.StringValue(n13.Name.Value)
				m12.Port = types.StringValue(n13.Port.Value)
			}
			m2.SourceServices = append(m2.SourceServices, m12)
		}
		for _, e14 := range e1.Node.Destination_services.Edges {
			var m15 firewallPolicyRulesDestinationServicesModel
			switch n16 := e14.Node.(type) {
			case *infrahub_sdk.FirewallPolicyRulesSecurityPolicyRulePaginatedSecurityPolicyRuleEdgesEdgedSecurityPolicyRuleNodeSecurityPolicyRuleDestination_servicesNestedPaginatedSecurityGenericServiceEdgesNestedEdgedSecurityGenericServiceNodeSecurityService:
				m15.Typename = types.StringValue(n16.GetTypename())
				m15.Name = types.StringValue(n16.Name.Value)
				m15.Port = types.StringValue(n16.Port.Value)
			}
			m2.DestinationServices = append(m2.DestinationServices, m15)
		}
		state.SecurityPolicyRule = append(state.SecurityPolicyRule, m2)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Configure adds the provider configured client to the data source.
func (d *firewallPolicyRulesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	// Add a nil check when handling ProviderData because Terraform
	// sets that data after it calls the ConfigureProvider RPC.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(graphql.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *graphql.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	d.client = &client
}
