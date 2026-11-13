package contract

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/common"
	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/api"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/utils"
)

func NewContractResource() resource.Resource {
	return &contractResource{}
}

type contractResource struct {
	client *api.PlatformClient
}

type contractResourceModel struct {
	Id                     types.String `tfsdk:"id"`
	Name                   types.String `tfsdk:"name"`
	SourceGraphName        types.String `tfsdk:"source"`
	Namespace              types.String `tfsdk:"namespace"`
	RoutingURL             types.String `tfsdk:"routing_url"`
	ExcludeTags            types.List   `tfsdk:"exclude_tags"`
	IncludeTags            types.List   `tfsdk:"include_tags"`
	Readme                 types.String `tfsdk:"readme"`
	AdmissionWebhookUrl    types.String `tfsdk:"admission_webhook_url"`
	AdmissionWebhookSecret types.String `tfsdk:"admission_webhook_secret"`
	SupportsFederation     types.Bool   `tfsdk:"supports_federation"`
	// These two only affect how the contract is created; Cosmo doesn't store them.
	CreateTempSubgraphIfNeeded  types.Bool `tfsdk:"create_temp_subgraph_if_needed"`
	AllowRecomposingOtherGraphs types.Bool `tfsdk:"allow_recomposing_other_graphs"`
}

func (r *contractResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contract"
}

func (r *contractResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `
A contract is a Terraform resource representing a single subgraph with GraphQL Federation enabled, allowing developers to build versatile, multi-audience graphs while simplifying development and ensuring maintainability. 

For more information, refer to the Cosmo Documentation at https://cosmo-docs.wundergraph.com/concepts/schema-contracts.
		`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"namespace": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"source": schema.StringAttribute{
				Required: true,
			},
			"exclude_tags": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
			},
			"include_tags": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
			},
			"readme": schema.StringAttribute{
				Optional: true,
			},
			"admission_webhook_url": schema.StringAttribute{
				Optional: true,
			},
			"admission_webhook_secret": schema.StringAttribute{
				Optional: true,
			},
			"routing_url": schema.StringAttribute{
				Required: true,
			},
			"supports_federation": schema.BoolAttribute{
				Optional: true,
				Computed: true,
			},
			"create_temp_subgraph_if_needed": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "If `true`, and the source graph has not composed and has no subgraph with a published schema " +
					"when this contract is created, the provider creates the contract with the help of a temporary subgraph, which " +
					"it deletes again straight afterwards. Defaults to `false`. Only used when the contract is created; changing it " +
					"later has no effect. **Leaves a dummy schema on the source graph's and the contract's routers until a real " +
					"subgraph is published.** See [Creating a contract before the source graph has any subgraphs]" +
					"(#creating-a-contract-before-the-source-graph-has-any-subgraphs) for how it works and its risks.",
			},
			"allow_recomposing_other_graphs": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Only used with `create_temp_subgraph_if_needed`. If `false` (the default), the provider refuses " +
					"to use a temporary subgraph if that would recompose any graph or contract other than the source graph. If `true`, " +
					"it goes ahead anyway and lists what was recomposed in a warning. **Dangerous:** a recomposed graph that is in use " +
					"briefly serves the dummy `_tempContractBootstrap` field to its routers, and is recomposed again when the temporary " +
					"subgraph is deleted. Existing contracts of the source graph count as other graphs, so this must be `true` to " +
					"create a second contract this way while the source graph is still empty.",
			},
		},
	}
}

func (r *contractResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*api.PlatformClient)
	if !ok {
		utils.AddDiagnosticError(resp,
			ErrUnexpectedResourceType,
			fmt.Sprintf("Expected *api.PlatformClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *contractResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data contractResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if data.CreateTempSubgraphIfNeeded.ValueBool() {
		unlock := lockSourceGraph(data.Namespace.ValueString(), data.SourceGraphName.ValueString())
		defer unlock()

		temp, diags := r.setUpTempSubgraph(ctx, data)
		resp.Diagnostics.Append(diags...)
		if diags.HasError() {
			return
		}
		if temp != nil {
			// Runs after the contract is created, whether or not that worked.
			defer func() {
				resp.Diagnostics.Append(r.removeTempSubgraph(ctx, temp)...)
				if len(temp.affected) > 0 {
					resp.Diagnostics.AddWarning("Other Graphs Recomposed", fmt.Sprintf(
						"Creating contract %q used a temporary subgraph, which also recomposed (twice): %s.",
						data.Name.ValueString(), strings.Join(temp.affected, ", ")))
				}
			}()
		}
	}

	response, apiError := r.createAndFetchContract(ctx, data, resp)
	if apiError != nil {
		if !api.IsSubgraphCompositionFailedError(apiError) {
			// returning only for a non composition error, as the resource exists otherwise
			return
		}
	}

	graph := response.Graph
	data.Id = types.StringValue(graph.GetId())
	data.Name = types.StringValue(graph.GetName())
	data.Namespace = types.StringValue(graph.GetNamespace())
	data.RoutingURL = types.StringValue(graph.GetRoutingURL())
	data.SupportsFederation = types.BoolValue(graph.GetSupportsFederation())

	if graph.Contract != nil && len(graph.Contract.GetExcludeTags()) > 0 {
		var responseExcludeTags []attr.Value
		for _, tag := range graph.Contract.GetExcludeTags() {
			responseExcludeTags = append(responseExcludeTags, types.StringValue(tag))
		}
		data.ExcludeTags = types.ListValueMust(types.StringType, responseExcludeTags)
	}

	if graph.Contract != nil && len(graph.Contract.GetIncludeTags()) > 0 {
		var responseIncludeTags []attr.Value
		for _, tag := range graph.Contract.IncludeTags {
			responseIncludeTags = append(responseIncludeTags, types.StringValue(tag))
		}
		data.IncludeTags = types.ListValueMust(types.StringType, responseIncludeTags)
	}

	if graph.Readme != nil {
		data.Readme = types.StringValue(*graph.Readme)
	}

	if graph.GetAdmissionWebhookUrl() != "" {
		data.AdmissionWebhookUrl = types.StringValue(*graph.AdmissionWebhookUrl)
	}

	utils.LogAction(ctx, DebugCreate, data.Id.ValueString(), data.Name.ValueString(), data.Namespace.ValueString())

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *contractResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data contractResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if data.Id.IsNull() || data.Id.ValueString() == "" {
		utils.AddDiagnosticError(resp, ErrInvalidResourceID, "Cannot read federated graph without an ID.")
		return
	}

	var graph *platformv1.FederatedGraph

	if data.Name.ValueString() == "" {
		response, apiError := r.client.GetFederatedGraphById(ctx, data.Id.ValueString())
		if apiError != nil {
			if api.IsNotFoundError(apiError) {
				utils.AddDiagnosticWarning(resp,
					ErrContractNotFound,
					apiError.Error(),
				)
				resp.State.RemoveResource(ctx)
				return
			}
			utils.AddDiagnosticError(resp, ErrReadingContract, apiError.Error())
			return
		}

		graph = response.Graph
	} else {
		response, apiError := r.client.GetFederatedGraph(ctx, data.Name.ValueString(), data.Namespace.ValueString())
		if apiError != nil {
			if api.IsNotFoundError(apiError) {
				utils.AddDiagnosticError(resp,
					ErrReadingContract,
					apiError.Error(),
				)
				resp.State.RemoveResource(ctx)
				return
			}
			utils.AddDiagnosticError(resp,
				ErrReadingContract,
				apiError.Error(),
			)
			return
		}
		graph = response.Graph
	}

	if graph.GetContract() == nil {
		utils.AddDiagnosticError(resp,
			ErrReadingContract,
			"Contract not found",
		)
		return
	}

	sourceGraphRes, err := r.client.GetFederatedGraphById(ctx, graph.GetContract().GetSourceFederatedGraphId())
	if err != nil {
		utils.AddDiagnosticError(resp,
			ErrReadingContract,
			err.Error(),
		)
		return
	}

	data.Id = types.StringValue(graph.GetId())
	data.Name = types.StringValue(graph.GetName())
	data.Namespace = types.StringValue(graph.GetNamespace())
	data.RoutingURL = types.StringValue(graph.GetRoutingURL())
	data.SupportsFederation = types.BoolValue(graph.GetSupportsFederation())
	data.SourceGraphName = types.StringValue(sourceGraphRes.Graph.GetName())

	if graph.Contract != nil && len(graph.Contract.GetExcludeTags()) > 0 {
		var responseExcludeTags []attr.Value
		for _, tag := range graph.Contract.GetExcludeTags() {
			responseExcludeTags = append(responseExcludeTags, types.StringValue(tag))
		}
		data.ExcludeTags = types.ListValueMust(types.StringType, responseExcludeTags)
	}

	if graph.Contract != nil && len(graph.Contract.GetIncludeTags()) > 0 {
		var responseIncludeTags []attr.Value
		for _, tag := range graph.Contract.IncludeTags {
			responseIncludeTags = append(responseIncludeTags, types.StringValue(tag))
		}
		data.IncludeTags = types.ListValueMust(types.StringType, responseIncludeTags)
	}

	if graph.Readme != nil {
		data.Readme = types.StringValue(*graph.Readme)
	}

	if graph.GetAdmissionWebhookUrl() != "" {
		data.AdmissionWebhookUrl = types.StringValue(*graph.AdmissionWebhookUrl)
	}

	// Not stored in Cosmo: keep what's in state, and default to false (e.g. after an import).
	if data.CreateTempSubgraphIfNeeded.IsNull() {
		data.CreateTempSubgraphIfNeeded = types.BoolValue(false)
	}
	if data.AllowRecomposingOtherGraphs.IsNull() {
		data.AllowRecomposingOtherGraphs = types.BoolValue(false)
	}

	if data.CreateTempSubgraphIfNeeded.ValueBool() {
		subgraphName := tempSubgraphName(data.Name.ValueString())
		if subgraph, apiError := r.client.GetSubgraph(ctx, subgraphName, data.Namespace.ValueString()); apiError == nil && subgraph != nil {
			utils.AddDiagnosticWarning(resp, "Temporary Subgraph Left Behind", fmt.Sprintf(
				"Temporary subgraph %q in namespace %q was created to help create this contract, but was never deleted "+
					"(for example, because an apply was interrupted). Its dummy Query field is part of the graphs it belongs to. "+
					"Delete it in Cosmo Studio or with: wgc subgraph delete %s -n %s",
				subgraphName, data.Namespace.ValueString(), subgraphName, data.Namespace.ValueString()))
		}
	}

	utils.LogAction(ctx, "read", data.Id.ValueString(), data.Name.ValueString(), data.Namespace.ValueString())

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *contractResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data contractResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	excludeTags, err := utils.ConvertLabelMatchers(data.ExcludeTags)
	if err != nil {
		utils.AddDiagnosticError(resp,
			ErrUpdatingContract,
			err.Error(),
		)
		return
	}

	includeTags, err := utils.ConvertLabelMatchers(data.IncludeTags)
	if err != nil {
		utils.AddDiagnosticError(resp,
			ErrCreatingContract,
			"Could not create contract: "+err.Error(),
		)
		return
	}

	if len(includeTags) > 0 && len(excludeTags) > 0 {
		utils.AddDiagnosticError(resp, ErrUpdatingContract, "a contract cannot have both the include and exclude tags")
		return
	}

	readme := utils.GetValueOrDefault(data.Readme.ValueStringPointer(), "")
	admissionWebhookUrl := utils.GetValueOrDefault(data.AdmissionWebhookUrl.ValueStringPointer(), "")
	admissionWebhookSecret := utils.GetValueOrDefault(data.AdmissionWebhookSecret.ValueStringPointer(), "")

	routingUrl := data.RoutingURL.ValueString()
	requestData := &platformv1.UpdateContractRequest{
		Name:                   data.Name.ValueString(),
		Namespace:              data.Namespace.ValueString(),
		ExcludeTags:            excludeTags,
		IncludeTags:            includeTags,
		RoutingUrl:             &routingUrl,
		AdmissionWebhookUrl:    &admissionWebhookUrl,
		AdmissionWebhookSecret: &admissionWebhookSecret,
		Readme:                 &readme,
	}

	_, apiError := r.client.UpdateContract(ctx, requestData)
	if apiError != nil {
		if api.IsContractCompositionFailedError(apiError) || api.IsSubgraphCompositionFailedError(apiError) {
			utils.AddDiagnosticError(resp,
				ErrUpdatingContract,
				apiError.Error(),
			)
		} else {
			utils.AddDiagnosticError(resp,
				ErrUpdatingContract,
				apiError.Error(),
			)
			return
		}
	}

	response, apiError := r.client.GetFederatedGraph(ctx, data.Name.ValueString(), data.Namespace.ValueString())
	if apiError != nil {
		utils.AddDiagnosticError(resp,
			ErrRetrievingContract,
			apiError.Error(),
		)
		return
	}

	graph := response.Graph
	data.Id = types.StringValue(graph.GetId())
	data.Name = types.StringValue(graph.GetName())
	data.Namespace = types.StringValue(graph.GetNamespace())
	data.RoutingURL = types.StringValue(graph.GetRoutingURL())
	data.SupportsFederation = types.BoolValue(graph.GetSupportsFederation())

	if graph.Contract != nil && len(graph.Contract.GetExcludeTags()) > 0 {
		var responseExcludeTags []attr.Value
		for _, tag := range graph.Contract.GetExcludeTags() {
			responseExcludeTags = append(responseExcludeTags, types.StringValue(tag))
		}
		data.ExcludeTags = types.ListValueMust(types.StringType, responseExcludeTags)
	}

	if graph.Contract != nil && len(graph.Contract.GetIncludeTags()) > 0 {
		var responseIncludeTags []attr.Value
		for _, tag := range graph.Contract.IncludeTags {
			responseIncludeTags = append(responseIncludeTags, types.StringValue(tag))
		}
		data.IncludeTags = types.ListValueMust(types.StringType, responseIncludeTags)
	}

	if graph.Readme != nil {
		data.Readme = types.StringValue(*graph.Readme)
	}

	if graph.GetAdmissionWebhookUrl() != "" {
		data.AdmissionWebhookUrl = types.StringValue(*graph.AdmissionWebhookUrl)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *contractResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data contractResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	apiError := r.client.DeleteContract(ctx, data.Name.ValueString(), data.Namespace.ValueString(), data.SupportsFederation.ValueBool())
	if apiError != nil {
		if api.IsNotFoundError(apiError) {
			utils.AddDiagnosticError(resp,
				ErrDeletingContract,
				apiError.Error(),
			)
			resp.State.RemoveResource(ctx)
		} else {
			utils.AddDiagnosticError(resp,
				ErrDeletingContract,
				apiError.Error(),
			)
			return
		}
	}

	utils.LogAction(ctx, "deleted contract", data.Id.ValueString(), data.Name.ValueString(), data.Namespace.ValueString())
}

func (r *contractResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *contractResource) createAndFetchContract(ctx context.Context, data contractResourceModel, resp *resource.CreateResponse) (*platformv1.GetFederatedGraphByNameResponse, *api.ApiError) {
	excludeTags, err := utils.ConvertLabelMatchers(data.ExcludeTags)
	if err != nil {
		utils.AddDiagnosticError(resp,
			ErrCreatingContract,
			"Could not create contract: "+err.Error(),
		)
		return nil, &api.ApiError{Err: err, Reason: "CreateContract", Status: common.EnumStatusCode_ERR}
	}

	includeTags, err := utils.ConvertLabelMatchers(data.IncludeTags)
	if err != nil {
		utils.AddDiagnosticError(resp,
			ErrCreatingContract,
			"Could not create contract: "+err.Error(),
		)
		return nil, &api.ApiError{Err: err, Reason: "CreateContract", Status: common.EnumStatusCode_ERR}
	}

	if len(includeTags) > 0 && len(excludeTags) > 0 {
		utils.AddDiagnosticError(resp, ErrCreatingContract, "a contract cannot have both the include and exclude tags")
		return nil, &api.ApiError{Err: err, Reason: "CreateContract", Status: common.EnumStatusCode_ERR}
	}

	utils.DebugAction(ctx, DebugCreate, data.Name.ValueString(), data.Namespace.ValueString(), map[string]interface{}{
		"routing_url": data.RoutingURL.ValueString(),
		"excludeTags": strings.Join(excludeTags, ","),
		"includeTags": strings.Join(includeTags, ","),
	})

	readme := data.Readme.ValueString()
	requestData := &platformv1.CreateContractRequest{
		Name:                   data.Name.ValueString(),
		Namespace:              data.Namespace.ValueString(),
		SourceGraphName:        data.SourceGraphName.ValueString(),
		RoutingUrl:             data.RoutingURL.ValueString(),
		AdmissionWebhookUrl:    data.AdmissionWebhookUrl.ValueString(),
		ExcludeTags:            excludeTags,
		Readme:                 &readme,
		AdmissionWebhookSecret: data.AdmissionWebhookSecret.ValueStringPointer(),
		IncludeTags:            includeTags,
	}

	_, apiError := r.client.CreateContract(ctx, requestData)
	if apiError != nil {
		if api.IsContractCompositionFailedError(apiError) || api.IsSubgraphCompositionFailedError(apiError) {
			utils.AddDiagnosticError(resp,
				ErrCreatingContract,
				"Contract composition failed: "+apiError.Error(),
			)
		} else {
			utils.AddDiagnosticError(resp,
				ErrCreatingContract,
				"Could not create contract: "+apiError.Error(),
			)
			return nil, apiError
		}
	}

	response, apiError := r.client.GetFederatedGraph(ctx, data.Name.ValueString(), data.Namespace.ValueString())
	if apiError != nil {
		return nil, apiError
	}

	utils.DebugAction(ctx, DebugCreate, data.Name.ValueString(), data.Namespace.ValueString(), map[string]interface{}{
		"id":    response.Graph.GetId(),
		"graph": response.Graph,
	})

	return response, nil
}
