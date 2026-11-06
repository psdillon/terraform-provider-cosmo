package organization_group

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/api"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/utils"
)

var _ datasource.DataSource = &OrganizationGroupDataSource{}

func NewOrganizationGroupDataSource() datasource.DataSource {
	return &OrganizationGroupDataSource{}
}

type OrganizationGroupDataSource struct {
	client *api.PlatformClient
}

type OrganizationGroupDataSourceModel struct {
	Id          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Builtin     types.Bool   `tfsdk:"builtin"`
	Rules       types.Set    `tfsdk:"rules"`
}

func (d *OrganizationGroupDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_group"
}

func (d *OrganizationGroupDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Retrieves an organization group by name, including the built-in `admin` group, which cannot be managed with the `cosmo_organization_group` resource. Use it to look up the ID of an existing group, for example to assign it to a `cosmo_api_key`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the group.",
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the group.",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "The description of the group.",
				Computed:            true,
			},
			"builtin": schema.BoolAttribute{
				MarkdownDescription: "Whether the group is one of the organization's built-in groups.",
				Computed:            true,
			},
			"rules": schema.SetNestedAttribute{
				MarkdownDescription: "The roles granted by the group.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"role": schema.StringAttribute{
							MarkdownDescription: "The granted role.",
							Computed:            true,
						},
						"namespaces": schema.SetAttribute{
							MarkdownDescription: "Namespace IDs the role is limited to. Null when not limited by namespace.",
							Computed:            true,
							ElementType:         types.StringType,
						},
						"resources": schema.SetAttribute{
							MarkdownDescription: "Target IDs the role is limited to. Null when not limited by resource.",
							Computed:            true,
							ElementType:         types.StringType,
						},
					},
				},
			},
		},
	}
}

func (d *OrganizationGroupDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*api.PlatformClient)
	if !ok {
		utils.AddDiagnosticError(resp, ErrUnexpectedDataSourceType, fmt.Sprintf("Expected *api.PlatformClient, got: %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}

	d.client = client
}

func (d *OrganizationGroupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data OrganizationGroupDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group, apiError := d.client.GetOrganizationGroup(ctx, "", data.Name.ValueString())
	if apiError != nil {
		utils.AddDiagnosticError(resp, ErrReadingOrganizationGroup, apiError.Error())
		return
	}

	rules, diags := rulesToSet(ctx, group.GetRules())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.Id = types.StringValue(group.GetGroupId())
	data.Name = types.StringValue(group.GetName())
	data.Description = types.StringValue(group.GetDescription())
	data.Builtin = types.BoolValue(group.GetBuiltin())
	data.Rules = rules

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
