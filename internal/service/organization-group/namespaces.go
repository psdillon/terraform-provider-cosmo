package organization_group

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/api"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/utils"
)

// Rules take namespace IDs only. Cosmo handles other values badly: a name makes the update fail with a
// generic internal error after the group's roles have already been changed in Keycloak, and an ID
// that doesn't exist is silently left out of the rule (which then shows up as "inconsistent result
// after apply"). So the provider checks the values before sending them. It lists the namespaces only
// to make the error more helpful: to say whether a value exists, and to give the ID of a namespace
// that was given by name. Names are never turned into IDs.

const (
	ErrInvalidNamespaceID     = "Invalid Namespace ID"
	ErrNamespaceIDsNotChecked = "Namespace IDs Not Checked"
)

// ruleNamespaces is the known namespace values of one rule.
type ruleNamespaces struct {
	role   string
	values []string
}

// knownRuleNamespaces returns the namespace values of each rule that are known. Unknown values (for
// example the ID of a namespace created in the same apply, during plan) are skipped.
func knownRuleNamespaces(ctx context.Context, rules types.Set) ([]ruleNamespaces, diag.Diagnostics) {
	if rules.IsNull() || rules.IsUnknown() {
		return nil, nil
	}

	var models []GroupRuleModel
	diags := rules.ElementsAs(ctx, &models, false)
	if diags.HasError() {
		return nil, diags
	}

	var result []ruleNamespaces
	for _, m := range models {
		if m.Namespaces.IsNull() || m.Namespaces.IsUnknown() {
			continue
		}
		var values []string
		for _, element := range m.Namespaces.Elements() {
			value, ok := element.(types.String)
			if !ok || value.IsNull() || value.IsUnknown() {
				continue
			}
			values = append(values, value.ValueString())
		}
		if len(values) > 0 {
			result = append(result, ruleNamespaces{role: m.Role.ValueString(), values: values})
		}
	}
	return result, diags
}

// checkRuleNamespaces checks that every known namespace value in rules is the ID of an existing
// namespace.
func (r *OrganizationGroupResource) checkRuleNamespaces(ctx context.Context, rules types.Set) diag.Diagnostics {
	values, diags := knownRuleNamespaces(ctx, rules)
	if diags.HasError() || len(values) == 0 {
		return diags
	}

	namespaces, apiError := r.client.GetNamespaces(ctx)
	if apiError != nil {
		diags.Append(namespaceDiagnostics(values, nil, apiError)...)
		return diags
	}

	diags.Append(namespaceDiagnostics(values, namespaces, nil)...)
	return diags
}

// namespaceDiagnostics reports the values that aren't IDs of existing namespaces. namespaces is what
// Cosmo listed; when listing failed (listErr is set), only the format of the values is checked.
func namespaceDiagnostics(rules []ruleNamespaces, namespaces []*platformv1.Namespace, listErr *api.ApiError) diag.Diagnostics {
	var diags diag.Diagnostics
	attr := path.Root("rules")

	ids := make(map[string]bool, len(namespaces))
	nameToID := make(map[string]string, len(namespaces))
	for _, ns := range namespaces {
		ids[ns.GetId()] = true
		nameToID[ns.GetName()] = ns.GetId()
	}

	unchecked := false
	for _, rule := range rules {
		for _, value := range rule.values {
			isID := utils.IsUUID(value)

			switch {
			case !isID && listErr == nil && nameToID[value] != "":
				diags.AddAttributeError(attr, ErrInvalidNamespaceID, fmt.Sprintf(
					"The rule for role %q lists %q, which is a namespace name. Rules take namespace IDs, not names. "+
						"Use the namespace's id attribute: cosmo_namespace.<resource name>.id, or "+
						"data.cosmo_namespace.<data source name>.id for a namespace Terraform doesn't manage. "+
						"The ID of namespace %q is %s.",
					rule.role, value, value, nameToID[value]))

			case !isID:
				detail := fmt.Sprintf(
					"The rule for role %q lists %q, which is not a namespace ID. Rules take namespace IDs, not names "+
						"(for example cosmo_namespace.example.id, or data.cosmo_namespace.example.id for a namespace "+
						"Terraform doesn't manage).",
					rule.role, value)
				if listErr == nil {
					detail += fmt.Sprintf(" No namespace is named %q either.", value)
				} else {
					detail += fmt.Sprintf(" (The provider couldn't list the namespaces to look %q up: %s)", value, listErr.Error())
				}
				diags.AddAttributeError(attr, ErrInvalidNamespaceID, detail)

			case listErr != nil:
				unchecked = true

			case !ids[value]:
				diags.AddAttributeError(attr, ErrInvalidNamespaceID, fmt.Sprintf(
					"The rule for role %q lists namespace ID %q, but no namespace with that ID exists in the "+
						"organization, or the API key can't see it. Cosmo would leave it out of the rule. "+
						"Check that the namespace hasn't been deleted, and that the ID belongs to this organization.",
					rule.role, value))
			}
		}
	}

	if unchecked {
		diags.AddAttributeWarning(attr, ErrNamespaceIDsNotChecked, fmt.Sprintf(
			"The provider couldn't list the namespaces to check that the rules' namespace IDs exist: %s. "+
				"Cosmo leaves out IDs that don't exist, which makes the apply fail with "+
				"\"Provider produced inconsistent result after apply\".",
			listErr.Error()))
	}

	return diags
}
