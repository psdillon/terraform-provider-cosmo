package api_key

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/api"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/utils"
)

var (
	_ resource.Resource                = (*ApiKeyResource)(nil)
	_ resource.ResourceWithConfigure   = (*ApiKeyResource)(nil)
	_ resource.ResourceWithImportState = (*ApiKeyResource)(nil)
)

type ApiKeyResource struct {
	client *api.PlatformClient
}

type ApiKeyResourceModel struct {
	Id          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	GroupId     types.String `tfsdk:"group_id"`
	Expires     types.String `tfsdk:"expires"`
	Permissions types.Set    `tfsdk:"permissions"`
	Key         types.String `tfsdk:"key"`
	ExpiresAt   types.String `tfsdk:"expires_at"`
	CreatedAt   types.String `tfsdk:"created_at"`
	CreatedBy   types.String `tfsdk:"created_by"`
}

func NewApiKeyResource() resource.Resource {
	return &ApiKeyResource{}
}

func (r *ApiKeyResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

func (r *ApiKeyResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `
Manages an organization API key in Cosmo, for use with the Cosmo CLI (` + "`wgc`" + `), CI pipelines or this provider. The key receives the permissions of the organization group it is assigned to.

The secret ` + "`key`" + ` is only returned by Cosmo when the key is created. It is stored in the Terraform state, so protect your state accordingly. Imported keys have no ` + "`key`" + ` value.

Requires an API key with the ` + "`organization-admin`" + ` or ` + "`organization-apikey-manager`" + ` role. Only organization admins can create keys in a group that grants ` + "`organization-admin`" + `.

For more information on API keys, please refer to the [Cosmo Documentation](https://cosmo-docs.wundergraph.com/studio/api-keys).
		`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the API key.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the API key, unique within the organization (3 to 50 characters). Changing this recreates the key.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthBetween(3, 50),
				},
			},
			"group_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the organization group whose permissions the key receives, for example `cosmo_organization_group.example.id`. This can be changed without recreating the key.",
				Required:            true,
			},
			"expires": schema.StringAttribute{
				MarkdownDescription: fmt.Sprintf("How long the key is valid for, counted from creation. One of `%s`, `%s`, `%s` or `%s`. Defaults to `%s`. Changing this recreates the key.",
					api.ApiKeyExpiresNever, api.ApiKeyExpiresThirtyDays, api.ApiKeyExpiresSixMonths, api.ApiKeyExpiresOneYear, api.ApiKeyExpiresNever),
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(api.ApiKeyExpiresNever),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf(api.ApiKeyExpiresNever, api.ApiKeyExpiresThirtyDays, api.ApiKeyExpiresSixMonths, api.ApiKeyExpiresOneYear),
				},
			},
			"permissions": schema.SetAttribute{
				MarkdownDescription: "Additional permissions for the key, such as `scim`. Cosmo does not return permissions when reading a key, so changes made outside Terraform are not detected. Changing this recreates the key.",
				Optional:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.RequiresReplace(),
				},
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "The secret API key value. Only available for keys created by Terraform.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"expires_at": schema.StringAttribute{
				MarkdownDescription: "When the key expires (RFC 3339). Empty if the key never expires.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "When the key was created (RFC 3339).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_by": schema.StringAttribute{
				MarkdownDescription: "The user who created the key.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *ApiKeyResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ApiKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ApiKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	expires, err := api.ResolveApiKeyExpiry(data.Expires.ValueString())
	if err != nil {
		utils.AddDiagnosticError(resp, ErrCreatingApiKey, err.Error())
		return
	}

	var permissions []string
	if !data.Permissions.IsNull() && !data.Permissions.IsUnknown() {
		resp.Diagnostics.Append(data.Permissions.ElementsAs(ctx, &permissions, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		sort.Strings(permissions)
	}

	key, apiError := r.client.CreateAPIKey(ctx, data.Name.ValueString(), data.GroupId.ValueString(), expires, permissions)
	if apiError != nil {
		utils.AddDiagnosticError(resp, ErrCreatingApiKey, apiError.Error())
		return
	}

	data.Key = types.StringValue(key)

	if apiError := r.refresh(ctx, &data); apiError != nil {
		// The key exists; keep it (and its secret) in state as tainted rather than orphaning it.
		data.Id = types.StringValue(data.Name.ValueString())
		data.ExpiresAt = types.StringValue("")
		data.CreatedAt = types.StringValue("")
		data.CreatedBy = types.StringValue("")
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		utils.AddDiagnosticError(resp, ErrReadingApiKey, fmt.Sprintf("The API key was created but could not be read back: %s", apiError.Error()))
		return
	}

	utils.LogAction(ctx, "created", data.Id.ValueString(), data.Name.ValueString(), "")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ApiKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ApiKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if apiError := r.refresh(ctx, &data); apiError != nil {
		if api.IsNotFoundError(apiError) {
			resp.State.RemoveResource(ctx)
			return
		}
		utils.AddDiagnosticError(resp, ErrReadingApiKey, apiError.Error())
		return
	}

	utils.LogAction(ctx, "read", data.Id.ValueString(), data.Name.ValueString(), "")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update only runs for group_id changes; every other argument forces replacement.
func (r *ApiKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ApiKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiError := r.client.UpdateAPIKey(ctx, data.Name.ValueString(), data.GroupId.ValueString())
	if apiError != nil {
		utils.AddDiagnosticError(resp, ErrUpdatingApiKey, apiError.Error())
		return
	}

	if apiError := r.refresh(ctx, &data); apiError != nil {
		utils.AddDiagnosticError(resp, ErrReadingApiKey, apiError.Error())
		return
	}

	utils.LogAction(ctx, "updated", data.Id.ValueString(), data.Name.ValueString(), "")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ApiKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ApiKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiError := r.client.DeleteAPIKey(ctx, data.Name.ValueString())
	if apiError != nil {
		if api.IsNotFoundError(apiError) {
			return
		}
		utils.AddDiagnosticError(resp, ErrDeletingApiKey, apiError.Error())
		return
	}

	utils.LogAction(ctx, "deleted", data.Id.ValueString(), data.Name.ValueString(), "")
}

// ImportState takes the API key name. The secret cannot be recovered, so `key` stays null.
func (r *ApiKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

// refresh reads the key by name into data. key and permissions are not returned
// by the API and are left as they are.
func (r *ApiKeyResource) refresh(ctx context.Context, data *ApiKeyResourceModel) *api.ApiError {
	apiKey, apiError := r.client.GetAPIKey(ctx, data.Name.ValueString())
	if apiError != nil {
		return apiError
	}

	data.Id = types.StringValue(apiKey.GetId())
	data.Name = types.StringValue(apiKey.GetName())
	data.GroupId = types.StringValue(apiKey.GetGroup().GetId())
	data.ExpiresAt = types.StringValue(apiKey.GetExpiresAt())
	data.CreatedAt = types.StringValue(apiKey.GetCreatedAt())
	data.CreatedBy = types.StringValue(apiKey.GetCreatedBy())

	// After an import there is no configured expiry yet; work it out from the timestamps
	// so the imported key does not plan a replacement.
	if data.Expires.IsNull() || data.Expires.IsUnknown() {
		data.Expires = types.StringValue(inferExpiry(apiKey))
	}

	return nil
}

// inferExpiry maps the stored expiry date back to the duration it was created with.
func inferExpiry(apiKey *platformv1.APIKey) string {
	if apiKey.GetExpiresAt() == "" {
		return api.ApiKeyExpiresNever
	}

	createdAt, errCreated := time.Parse(time.RFC3339, apiKey.GetCreatedAt())
	expiresAt, errExpires := time.Parse(time.RFC3339, apiKey.GetExpiresAt())
	if errCreated != nil || errExpires != nil {
		return api.ApiKeyExpiresNever
	}

	days := expiresAt.Sub(createdAt).Hours() / 24
	switch {
	case days < 90:
		return api.ApiKeyExpiresThirtyDays
	case days < 270:
		return api.ApiKeyExpiresSixMonths
	default:
		return api.ApiKeyExpiresOneYear
	}
}
