package organization_group

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/common"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/api"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/utils"
)

var (
	_ resource.Resource                   = (*OrganizationGroupResource)(nil)
	_ resource.ResourceWithConfigure      = (*OrganizationGroupResource)(nil)
	_ resource.ResourceWithImportState    = (*OrganizationGroupResource)(nil)
	_ resource.ResourceWithValidateConfig = (*OrganizationGroupResource)(nil)
)

type OrganizationGroupResource struct {
	client *api.PlatformClient
}

type OrganizationGroupResourceModel struct {
	Id                   types.String `tfsdk:"id"`
	Name                 types.String `tfsdk:"name"`
	Description          types.String `tfsdk:"description"`
	Rules                types.Set    `tfsdk:"rules"`
	MoveMembersToGroupId types.String `tfsdk:"move_members_to_group_id"`
}

func NewOrganizationGroupResource() resource.Resource {
	return &OrganizationGroupResource{}
}

func (r *OrganizationGroupResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_group"
}

func (r *OrganizationGroupResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `
Manages an organization group (a set of role-based permissions) in Cosmo. Members and API keys are assigned to a group to receive its permissions.

Requires the RBAC feature to be enabled for the organization, and an API key with the ` + "`organization-admin`" + ` role.

For more information on groups, please refer to the [Cosmo Documentation](https://cosmo-docs.wundergraph.com/studio/groups).
		`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the group.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the group. Groups cannot be renamed, so changing this recreates the group.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A description of the group.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
			},
			"rules": schema.SetNestedAttribute{
				MarkdownDescription: "The roles granted by the group. Use at most one rule per role. Omit to create a group with no permissions.",
				Optional:            true,
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"role": schema.StringAttribute{
							MarkdownDescription: fmt.Sprintf("The role to grant. One of: `%s`.", joinRoles()),
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf(validRoles...),
							},
						},
						"namespaces": schema.SetAttribute{
							MarkdownDescription: "Namespace IDs (for example `cosmo_namespace.example.id`) the role is limited to. Omit to grant the role on all namespaces. Not allowed for the `organization-*` admin, developer and viewer roles, or together with `resources`.",
							Optional:            true,
							ElementType:         types.StringType,
							Validators: []validator.Set{
								setvalidator.SizeAtLeast(1),
							},
						},
						"resources": schema.SetAttribute{
							MarkdownDescription: "Target IDs of the federated graphs or subgraphs the role is limited to. Omit to grant the role on all resources. Not allowed for the `organization-*` admin, developer and viewer roles, or together with `namespaces`.",
							Optional:            true,
							ElementType:         types.StringType,
							Validators: []validator.Set{
								setvalidator.SizeAtLeast(1),
							},
						},
					},
				},
			},
			"move_members_to_group_id": schema.StringAttribute{
				MarkdownDescription: "Only used when the group is destroyed: the ID of the group to move this group's members, API keys and OIDC mappers to. The control plane refuses to delete a group that still has any of these unless this is set.",
				Optional:            true,
			},
		},
	}
}

func (r *OrganizationGroupResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ValidateConfig rejects rule combinations that the control plane would silently
// rewrite, which would otherwise surface as "inconsistent result after apply".
func (r *OrganizationGroupResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data OrganizationGroupResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || data.Rules.IsNull() || data.Rules.IsUnknown() {
		return
	}

	var rules []GroupRuleModel
	resp.Diagnostics.Append(data.Rules.ElementsAs(ctx, &rules, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	seenRoles := map[string]bool{}
	for _, rule := range rules {
		if rule.Role.IsUnknown() || rule.Role.IsNull() {
			continue
		}
		role := rule.Role.ValueString()

		if seenRoles[role] {
			resp.Diagnostics.AddAttributeError(path.Root("rules"), ErrInvalidOrganizationGroup,
				fmt.Sprintf("The role %q appears in more than one rule. Use a single rule per role and list all its namespaces or resources there.", role))
		}
		seenRoles[role] = true

		hasNamespaces := !rule.Namespaces.IsNull()
		hasResources := !rule.Resources.IsNull()

		if organizationWideRoles[role] && (hasNamespaces || hasResources) {
			resp.Diagnostics.AddAttributeError(path.Root("rules"), ErrInvalidOrganizationGroup,
				fmt.Sprintf("The role %q applies to the whole organization and cannot be limited with namespaces or resources.", role))
		}

		if hasNamespaces && hasResources {
			resp.Diagnostics.AddAttributeError(path.Root("rules"), ErrInvalidOrganizationGroup,
				fmt.Sprintf("The rule for role %q sets both namespaces and resources. Set only one of them.", role))
		}
	}
}

func (r *OrganizationGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data OrganizationGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rules, diags := rulesFromSet(ctx, data.Rules)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	group, apiError := r.client.CreateOrganizationGroup(ctx, data.Name.ValueString(), data.Description.ValueString())
	if apiError != nil {
		utils.AddDiagnosticError(resp, ErrCreatingOrganizationGroup, apiError.Error())
		return
	}

	data.Id = types.StringValue(group.GetGroupId())

	// The group exists from here on; record it so a failure below leaves it tainted rather than orphaned.
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Rules can only be set through an update.
	if len(rules) > 0 {
		apiError = r.client.UpdateOrganizationGroup(ctx, data.Id.ValueString(), data.Description.ValueString(), rules)
		if apiError != nil {
			utils.AddDiagnosticError(resp, ErrCreatingOrganizationGroup, fmt.Sprintf("The group was created but its rules could not be set: %s", apiError.Error()))
			return
		}
	}

	if apiError := r.refresh(ctx, &data); apiError != nil {
		utils.AddDiagnosticError(resp, ErrReadingOrganizationGroup, apiError.Error())
		return
	}

	utils.LogAction(ctx, "created", data.Id.ValueString(), data.Name.ValueString(), "")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *OrganizationGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data OrganizationGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if apiError := r.refresh(ctx, &data); apiError != nil {
		if api.IsNotFoundError(apiError) {
			resp.State.RemoveResource(ctx)
			return
		}
		utils.AddDiagnosticError(resp, ErrReadingOrganizationGroup, apiError.Error())
		return
	}

	utils.LogAction(ctx, "read", data.Id.ValueString(), data.Name.ValueString(), "")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *OrganizationGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data OrganizationGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rules, diags := rulesFromSet(ctx, data.Rules)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiError := r.client.UpdateOrganizationGroup(ctx, data.Id.ValueString(), data.Description.ValueString(), rules)
	if apiError != nil {
		utils.AddDiagnosticError(resp, ErrUpdatingOrganizationGroup, apiError.Error())
		return
	}

	if apiError := r.refresh(ctx, &data); apiError != nil {
		utils.AddDiagnosticError(resp, ErrReadingOrganizationGroup, apiError.Error())
		return
	}

	utils.LogAction(ctx, "updated", data.Id.ValueString(), data.Name.ValueString(), "")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *OrganizationGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data OrganizationGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var toGroupId *string
	if !data.MoveMembersToGroupId.IsNull() && data.MoveMembersToGroupId.ValueString() != "" {
		v := data.MoveMembersToGroupId.ValueString()
		toGroupId = &v
	}

	apiError := r.client.DeleteOrganizationGroup(ctx, data.Id.ValueString(), toGroupId)
	if apiError != nil {
		if api.IsNotFoundError(apiError) {
			return
		}
		utils.AddDiagnosticError(resp, ErrDeletingOrganizationGroup, apiError.Error())
		return
	}

	utils.LogAction(ctx, "deleted", data.Id.ValueString(), data.Name.ValueString(), "")
}

// ImportState accepts either the group ID or the group name.
func (r *OrganizationGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if err := uuid.Validate(req.ID); err == nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

// refresh loads the group (by ID, or by name when the ID is not yet known) into data.
// move_members_to_group_id is not stored by the API and is left untouched.
func (r *OrganizationGroupResource) refresh(ctx context.Context, data *OrganizationGroupResourceModel) *api.ApiError {
	group, apiError := r.client.GetOrganizationGroup(ctx, data.Id.ValueString(), data.Name.ValueString())
	if apiError != nil {
		return apiError
	}

	rules, diags := rulesToSet(ctx, group.GetRules())
	if diags.HasError() {
		return &api.ApiError{Err: fmt.Errorf("%v", diags), Reason: "converting group rules", Status: common.EnumStatusCode_ERR}
	}

	data.Id = types.StringValue(group.GetGroupId())
	data.Name = types.StringValue(group.GetName())
	data.Description = types.StringValue(group.GetDescription())
	data.Rules = rules

	return nil
}

func joinRoles() string {
	out := ""
	for i, role := range validRoles {
		if i > 0 {
			out += "`, `"
		}
		out += role
	}
	return out
}
