package organization_group

import (
	"context"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/api"
)

// Roles accepted by the control plane (organization_role enum).
const (
	RoleOrganizationAdmin         = "organization-admin"
	RoleOrganizationDeveloper     = "organization-developer"
	RoleOrganizationViewer        = "organization-viewer"
	RoleOrganizationApiKeyManager = "organization-apikey-manager"
	RoleNamespaceAdmin            = "namespace-admin"
	RoleNamespaceViewer           = "namespace-viewer"
	RoleGraphAdmin                = "graph-admin"
	RoleGraphViewer               = "graph-viewer"
	RoleSubgraphAdmin             = "subgraph-admin"
	RoleSubgraphPublisher         = "subgraph-publisher"
	RoleSubgraphChecker           = "subgraph-checker"
	RoleSubgraphViewer            = "subgraph-viewer"
)

var validRoles = []string{
	RoleOrganizationAdmin,
	RoleOrganizationDeveloper,
	RoleOrganizationViewer,
	RoleOrganizationApiKeyManager,
	RoleNamespaceAdmin,
	RoleNamespaceViewer,
	RoleGraphAdmin,
	RoleGraphViewer,
	RoleSubgraphAdmin,
	RoleSubgraphPublisher,
	RoleSubgraphChecker,
	RoleSubgraphViewer,
}

// organizationWideRoles apply to the whole organization; the control plane
// discards any namespaces or resources sent with them.
var organizationWideRoles = map[string]bool{
	RoleOrganizationAdmin:     true,
	RoleOrganizationDeveloper: true,
	RoleOrganizationViewer:    true,
}

// GroupRuleModel is one entry of the `rules` set.
type GroupRuleModel struct {
	Role       types.String `tfsdk:"role"`
	Namespaces types.Set    `tfsdk:"namespaces"`
	Resources  types.Set    `tfsdk:"resources"`
}

var groupRuleAttrTypes = map[string]attr.Type{
	"role":       types.StringType,
	"namespaces": types.SetType{ElemType: types.StringType},
	"resources":  types.SetType{ElemType: types.StringType},
}

var groupRuleObjectType = types.ObjectType{AttrTypes: groupRuleAttrTypes}

// rulesFromSet converts the Terraform `rules` set into API rules.
func rulesFromSet(ctx context.Context, rules types.Set) ([]api.OrganizationGroupRule, diag.Diagnostics) {
	var diags diag.Diagnostics
	if rules.IsNull() || rules.IsUnknown() {
		return nil, diags
	}

	var models []GroupRuleModel
	diags.Append(rules.ElementsAs(ctx, &models, false)...)
	if diags.HasError() {
		return nil, diags
	}

	result := make([]api.OrganizationGroupRule, 0, len(models))
	for _, m := range models {
		namespaces, d := stringsFromSet(ctx, m.Namespaces)
		diags.Append(d...)
		resources, d := stringsFromSet(ctx, m.Resources)
		diags.Append(d...)

		result = append(result, api.OrganizationGroupRule{
			Role:       m.Role.ValueString(),
			Namespaces: namespaces,
			Resources:  resources,
		})
	}

	return result, diags
}

// rulesToSet converts the rules returned by the API into the Terraform `rules` set.
// Empty lists become null so that omitted attributes do not show a diff.
func rulesToSet(ctx context.Context, rules []*platformv1.OrganizationGroupRule) (types.Set, diag.Diagnostics) {
	var diags diag.Diagnostics
	if len(rules) == 0 {
		return types.SetNull(groupRuleObjectType), diags
	}

	models := make([]GroupRuleModel, 0, len(rules))
	for _, rule := range rules {
		namespaces, d := stringsToSet(ctx, rule.GetNamespaces())
		diags.Append(d...)
		resources, d := stringsToSet(ctx, rule.GetResources())
		diags.Append(d...)

		models = append(models, GroupRuleModel{
			Role:       types.StringValue(rule.GetRole()),
			Namespaces: namespaces,
			Resources:  resources,
		})
	}

	set, d := types.SetValueFrom(ctx, groupRuleObjectType, models)
	diags.Append(d...)
	return set, diags
}

func stringsFromSet(ctx context.Context, set types.Set) ([]string, diag.Diagnostics) {
	if set.IsNull() || set.IsUnknown() {
		return []string{}, nil
	}

	var values []string
	diags := set.ElementsAs(ctx, &values, false)
	sort.Strings(values)
	return values, diags
}

func stringsToSet(ctx context.Context, values []string) (types.Set, diag.Diagnostics) {
	if len(values) == 0 {
		return types.SetNull(types.StringType), nil
	}
	return types.SetValueFrom(ctx, types.StringType, values)
}
