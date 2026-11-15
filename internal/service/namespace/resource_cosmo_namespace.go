package namespace

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/api"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/utils"
)

var (
	_ resource.Resource                = (*NamespaceResource)(nil)
	_ resource.ResourceWithImportState = (*NamespaceResource)(nil)
)

type NamespaceResource struct {
	client *api.PlatformClient
}

type NamespaceResourceModel struct {
	Id        types.String             `tfsdk:"id"`
	Name      types.String             `tfsdk:"name"`
	Proposals *NamespaceProposalsModel `tfsdk:"proposals"`
}

// NamespaceProposalsModel is the "proposals" attribute. It is nil when proposals
// are turned off for the namespace.
type NamespaceProposalsModel struct {
	CheckSeverityLevel   types.String `tfsdk:"check_severity_level"`
	PublishSeverityLevel types.String `tfsdk:"publish_severity_level"`
}

// Severity levels accepted by the "proposals" attribute. They match the names of
// platformv1.LintSeverity, and "error" is what Cosmo uses when proposals are turned on.
const (
	severityWarn  = "warn"
	severityError = "error"
)

func NewNamespaceResource() resource.Resource {
	return &NamespaceResource{}
}

func (r *NamespaceResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*api.PlatformClient)
	if !ok {
		utils.AddDiagnosticError(resp, ErrUnexpectedDataSourceType, fmt.Sprintf("Expected *api.PlatformClient, got: %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}

	r.client = client
}

func (r *NamespaceResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_namespace"
}

func (r *NamespaceResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `
Namespaces group federated graphs and subgraphs. Each organization has a default, non-deletable namespace. 

For more information on namespaces, please refer to the [Cosmo Documentation](https://cosmo-docs.wundergraph.com/cli/namespace).

Set ` + "`proposals`" + ` to turn on [schema proposals](https://cosmo-docs.wundergraph.com/concepts/proposals) for the namespace. With proposals on, Cosmo compares each subgraph schema check and publish in the namespace against the approved proposals. This includes the subgraph schemas this provider publishes. Proposals need a Cosmo plan that includes them.
		`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The unique identifier of the namespace resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The name of the namespace. Changing the name destroys the namespace and creates a new one.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"proposals": schema.SingleNestedAttribute{
				Optional: true,
				MarkdownDescription: "Turns on schema proposals for the namespace. Leave it out to keep proposals off; removing it turns them off " +
					"and Cosmo discards the severity levels. `proposals = {}` turns them on with both levels set to `error`.",
				Attributes: map[string]schema.Attribute{
					"check_severity_level": severityAttribute(
						"What happens when a subgraph schema check (`wgc subgraph check`) finds a schema that matches no approved or draft proposal: " +
							"`error` fails the check, `warn` only reports it. Defaults to `error`.",
					),
					"publish_severity_level": severityAttribute(
						"What happens when a subgraph schema publish (`wgc subgraph publish`, or a `cosmo_subgraph` with a `schema`) sends a schema " +
							"that matches no approved proposal: `error` rejects the publish, `warn` publishes it with a warning. Defaults to `error`.",
					),
				},
			},
		},
	}
}

func severityAttribute(description string) schema.StringAttribute {
	return schema.StringAttribute{
		Optional:            true,
		Computed:            true,
		Default:             stringdefault.StaticString(severityError),
		MarkdownDescription: description,
		Validators: []validator.String{
			stringvalidator.OneOf(severityWarn, severityError),
		},
	}
}

func (r *NamespaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data NamespaceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.Name.IsNull() || data.Name.ValueString() == "" {
		utils.AddDiagnosticError(resp, ErrInvalidNamespaceName, "The 'name' attribute is required.")
		return
	}

	apiError := r.client.CreateNamespace(ctx, data.Name.ValueString())
	if apiError != nil {
		utils.AddDiagnosticError(resp,
			ErrCreatingNamespace,
			apiError.Error(),
		)
		return
	}

	namespace, err := getNamespace(ctx, *r.client, data.Id.ValueString(), data.Name.ValueString())
	if err != nil {
		utils.AddDiagnosticError(resp,
			ErrReadingNamespace,
			err.Error(),
		)
		return
	}

	data.Id = types.StringValue(namespace.Id)
	data.Name = types.StringValue(namespace.Name)

	if data.Proposals != nil {
		if apiError := r.applyProposals(ctx, data.Name.ValueString(), nil, data.Proposals); apiError != nil {
			// The namespace exists, so record it without proposals. Terraform marks it
			// as tainted and replaces it on the next apply.
			data.Proposals = nil
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
			utils.AddDiagnosticError(resp, ErrConfiguringProposals, apiError.Error())
			return
		}
	}

	utils.LogAction(ctx, "created", data.Id.ValueString(), data.Name.ValueString(), "")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *NamespaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data NamespaceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	namespace, apiError := getNamespace(ctx, *r.client, data.Id.ValueString(), data.Name.ValueString())
	if apiError != nil {
		if api.IsNotFoundError(apiError) {
			resp.State.RemoveResource(ctx)
			return
		}
		utils.AddDiagnosticError(resp, ErrReadingNamespace, apiError.Error())
		return
	}

	data.Id = types.StringValue(namespace.Id)
	data.Name = types.StringValue(namespace.Name)

	proposals, apiError := r.readProposals(ctx, namespace.Name)
	if apiError != nil {
		utils.AddDiagnosticError(resp, ErrReadingNamespace, apiError.Error())
		return
	}
	data.Proposals = proposals

	utils.LogAction(ctx, "read", data.Id.ValueString(), data.Name.ValueString(), "")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update only ever changes "proposals": changing "name" makes Terraform replace
// the namespace instead (see RequiresReplace in Schema).
func (r *NamespaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state NamespaceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if apiError := r.applyProposals(ctx, state.Name.ValueString(), state.Proposals, plan.Proposals); apiError != nil {
		utils.AddDiagnosticError(resp, ErrConfiguringProposals, apiError.Error())
		return
	}

	utils.LogAction(ctx, "updated", plan.Id.ValueString(), plan.Name.ValueString(), "")

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *NamespaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data NamespaceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteNamespace(ctx, data.Name.ValueString())
	if err != nil {
		utils.AddDiagnosticError(resp,
			ErrDeletingNamespace,
			err.Error(),
		)
		return
	}

	utils.LogAction(ctx, "deleted", data.Id.ValueString(), data.Name.ValueString(), "")
}

// applyProposals moves a namespace's proposal setting from current to desired.
// A nil value means proposals are off.
func (r *NamespaceResource) applyProposals(ctx context.Context, namespace string, current, desired *NamespaceProposalsModel) *api.ApiError {
	if desired == nil {
		if current == nil {
			return nil
		}
		return r.client.EnableProposalsForNamespace(ctx, namespace, false)
	}

	if current == nil {
		// Turning proposals on resets both levels to "error", so set them afterwards.
		if apiError := r.client.EnableProposalsForNamespace(ctx, namespace, true); apiError != nil {
			return apiError
		}
	}

	return r.client.ConfigureNamespaceProposalConfig(ctx, namespace,
		severityFromString(desired.CheckSeverityLevel.ValueString()),
		severityFromString(desired.PublishSeverityLevel.ValueString()),
	)
}

// readProposals returns the namespace's proposal setting, or nil when proposals are off.
func (r *NamespaceResource) readProposals(ctx context.Context, namespace string) (*NamespaceProposalsModel, *api.ApiError) {
	config, apiError := r.client.GetNamespaceProposalConfig(ctx, namespace)
	if apiError != nil {
		// Cosmo refuses to read the setting when the organization's plan has no
		// proposals. Proposals cannot be on then, and namespaces that do not use
		// them must keep working on such plans.
		if api.IsUpgradePlanError(apiError) {
			return nil, nil
		}
		return nil, apiError
	}

	if !config.Enabled {
		return nil, nil
	}

	return &NamespaceProposalsModel{
		CheckSeverityLevel:   types.StringValue(config.CheckSeverityLevel.String()),
		PublishSeverityLevel: types.StringValue(config.PublishSeverityLevel.String()),
	}, nil
}

func severityFromString(level string) platformv1.LintSeverity {
	if level == severityWarn {
		return platformv1.LintSeverity_warn
	}
	return platformv1.LintSeverity_error
}

func getNamespace(ctx context.Context, client api.PlatformClient, id, name string) (*platformv1.Namespace, *api.ApiError) {
	namespace, err := client.GetNamespace(ctx, id, name)
	if err != nil {
		return nil, err
	}
	return namespace, nil
}

func (r *NamespaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var data NamespaceResourceModel

	id := req.ID

	if err := uuid.Validate(id); err != nil {
		data.Name = types.StringValue(id) // We assume this is the namespace name
	} else {
		data.Id = types.StringValue(id)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
