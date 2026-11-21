package namespace

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/api"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/utils"
)

var _ datasource.DataSource = &NamespacesDataSource{}

func NewNamespacesDataSource() datasource.DataSource {
	return &NamespacesDataSource{}
}

// NamespacesDataSource lists the namespaces the API key can see.
type NamespacesDataSource struct {
	client *api.PlatformClient
}

type NamespacesDataSourceModel struct {
	Namespaces []NamespaceDataSourceModel `tfsdk:"namespaces"`
}

func (d *NamespacesDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_namespaces"
}

func (d *NamespacesDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the namespaces that the provider's API key can see (all of them for an `organization-admin` key), sorted by name. " +
			"Use it to look up namespace IDs by name, for example for the `namespaces` of a `cosmo_organization_group` rule.",

		Attributes: map[string]schema.Attribute{
			"namespaces": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The namespaces, sorted by name.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "The unique identifier of the namespace.",
						},
						"name": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "The name of the namespace.",
						},
					},
				},
			},
		},
	}
}

func (d *NamespacesDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *NamespacesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	namespaces, apiError := d.client.GetNamespaces(ctx)
	if apiError != nil {
		utils.AddDiagnosticError(resp, ErrReadingNamespaces, apiError.Error())
		return
	}

	data := NamespacesDataSourceModel{
		Namespaces: make([]NamespaceDataSourceModel, 0, len(namespaces)),
	}
	for _, namespace := range namespaces {
		data.Namespaces = append(data.Namespaces, NamespaceDataSourceModel{
			Id:   types.StringValue(namespace.GetId()),
			Name: types.StringValue(namespace.GetName()),
		})
	}

	// Cosmo doesn't promise an order. Sorting keeps the list stable between runs.
	sort.Slice(data.Namespaces, func(i, j int) bool {
		return data.Namespaces[i].Name.ValueString() < data.Namespaces[j].Name.ValueString()
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
