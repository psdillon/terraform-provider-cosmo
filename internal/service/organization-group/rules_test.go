package organization_group

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
)

func TestRulesRoundTrip(t *testing.T) {
	ctx := context.Background()

	apiRules := []*platformv1.OrganizationGroupRule{
		{Role: RoleOrganizationViewer},
		{Role: RoleNamespaceAdmin, Namespaces: []string{"ns-2", "ns-1"}},
		{Role: RoleGraphAdmin, Resources: []string{"target-1"}},
	}

	set, diags := rulesToSet(ctx, apiRules)
	if diags.HasError() {
		t.Fatalf("rulesToSet: %v", diags)
	}
	if got := len(set.Elements()); got != 3 {
		t.Fatalf("expected 3 rules, got %d", got)
	}

	rules, diags := rulesFromSet(ctx, set)
	if diags.HasError() {
		t.Fatalf("rulesFromSet: %v", diags)
	}

	byRole := map[string][]string{}
	for _, r := range rules {
		byRole[r.Role] = append(append([]string{}, r.Namespaces...), r.Resources...)
	}

	if len(byRole[RoleOrganizationViewer]) != 0 {
		t.Errorf("organization-viewer should have no scope, got %v", byRole[RoleOrganizationViewer])
	}
	if got := byRole[RoleNamespaceAdmin]; len(got) != 2 || got[0] != "ns-1" || got[1] != "ns-2" {
		t.Errorf("namespace-admin namespaces = %v, want sorted [ns-1 ns-2]", got)
	}
	if got := byRole[RoleGraphAdmin]; len(got) != 1 || got[0] != "target-1" {
		t.Errorf("graph-admin resources = %v, want [target-1]", got)
	}
}

func TestRulesToSetEmptyIsNull(t *testing.T) {
	set, diags := rulesToSet(context.Background(), nil)
	if diags.HasError() {
		t.Fatalf("rulesToSet: %v", diags)
	}
	if !set.IsNull() {
		t.Errorf("expected null set for no rules, got %v", set)
	}
}

func TestRulesFromNullSet(t *testing.T) {
	rules, diags := rulesFromSet(context.Background(), types.SetNull(groupRuleObjectType))
	if diags.HasError() {
		t.Fatalf("rulesFromSet: %v", diags)
	}
	if len(rules) != 0 {
		t.Errorf("expected no rules, got %v", rules)
	}
}
