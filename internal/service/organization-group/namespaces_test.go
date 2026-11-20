package organization_group

import (
	"context"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/common"
	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1/platformv1connect"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/api"
)

const (
	devID     = "11111111-1111-4111-8111-111111111111"
	prodID    = "22222222-2222-4222-8222-222222222222"
	missingID = "33333333-3333-4333-8333-333333333333"
)

var testNamespaces = []*platformv1.Namespace{
	{Id: devID, Name: "dev"},
	{Id: prodID, Name: "prod"},
}

var errList = &api.ApiError{Err: errors.New("connection refused"), Reason: "GetNamespaces", Status: common.EnumStatusCode_ERR}

// summaries returns "severity: detail" for each diagnostic.
func summaries(diags diag.Diagnostics) []string {
	var out []string
	for _, d := range diags {
		out = append(out, d.Severity().String()+": "+d.Detail())
	}
	return out
}

func TestNamespaceDiagnostics(t *testing.T) {
	tests := []struct {
		name       string
		values     []string
		namespaces []*platformv1.Namespace
		listErr    *api.ApiError
		want       []string // one substring per expected diagnostic, in order
	}{
		{
			name:       "existing IDs",
			values:     []string{devID, prodID},
			namespaces: testNamespaces,
		},
		{
			name:       "name of an existing namespace gives its ID",
			values:     []string{"dev"},
			namespaces: testNamespaces,
			want:       []string{`Error: The rule for role "namespace-admin" lists "dev", which is a namespace name. Rules take namespace IDs, not names.`},
		},
		{
			name:       "name of an existing namespace mentions its ID",
			values:     []string{"prod"},
			namespaces: testNamespaces,
			want:       []string{`The ID of namespace "prod" is ` + prodID},
		},
		{
			name:       "unknown name",
			values:     []string{"staging"},
			namespaces: testNamespaces,
			want:       []string{`Error: The rule for role "namespace-admin" lists "staging", which is not a namespace ID. Rules take namespace IDs, not names`},
		},
		{
			name:       "unknown name says it isn't a name either",
			values:     []string{"staging"},
			namespaces: testNamespaces,
			want:       []string{`No namespace is named "staging" either.`},
		},
		{
			name:       "ID that doesn't exist",
			values:     []string{missingID},
			namespaces: testNamespaces,
			want:       []string{`Error: The rule for role "namespace-admin" lists namespace ID "` + missingID + `", but no namespace with that ID exists`},
		},
		{
			name:    "listing failed: a name is still an error, without the lookup",
			values:  []string{"dev"},
			listErr: errList,
			want:    []string{`Error: The rule for role "namespace-admin" lists "dev", which is not a namespace ID. Rules take namespace IDs, not names`},
		},
		{
			name:    "listing failed: the error says why the name wasn't looked up",
			values:  []string{"dev"},
			listErr: errList,
			want:    []string{`couldn't list the namespaces to look "dev" up`},
		},
		{
			name:    "listing failed: IDs get one warning",
			values:  []string{devID, missingID},
			listErr: errList,
			want:    []string{"Warning: The provider couldn't list the namespaces to check that the rules' namespace IDs exist"},
		},
		{
			name:       "only the visible namespaces were listed",
			values:     []string{devID, prodID},
			namespaces: testNamespaces[:1],
			want:       []string{`lists namespace ID "` + prodID + `", but no namespace with that ID exists in the organization, or the API key can't see it`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := []ruleNamespaces{{role: RoleNamespaceAdmin, values: tt.values}}

			got := summaries(namespaceDiagnostics(rules, tt.namespaces, tt.listErr))

			if len(got) != len(tt.want) {
				t.Fatalf("got %d diagnostics, want %d:\n%s", len(got), len(tt.want), strings.Join(got, "\n"))
			}
			for i, want := range tt.want {
				if !strings.Contains(got[i], want) {
					t.Errorf("diagnostic %d = %q, want it to contain %q", i, got[i], want)
				}
			}
		})
	}
}

func rule(t *testing.T, role string, namespaces types.Set) attr.Value {
	t.Helper()
	value, diags := types.ObjectValue(groupRuleAttrTypes, map[string]attr.Value{
		"role":       types.StringValue(role),
		"namespaces": namespaces,
		"resources":  types.SetNull(types.StringType),
	})
	if diags.HasError() {
		t.Fatalf("ObjectValue: %v", diags)
	}
	return value
}

func TestKnownRuleNamespacesSkipsUnknownValues(t *testing.T) {
	ctx := context.Background()
	rules, diags := types.SetValue(groupRuleObjectType, []attr.Value{
		rule(t, RoleNamespaceAdmin, types.SetValueMust(types.StringType, []attr.Value{types.StringValue("dev"), types.StringUnknown()})),
		rule(t, RoleNamespaceViewer, types.SetUnknown(types.StringType)),
		rule(t, RoleOrganizationViewer, types.SetNull(types.StringType)),
	})
	if diags.HasError() {
		t.Fatalf("SetValue: %v", diags)
	}

	got, diags := knownRuleNamespaces(ctx, rules)
	if diags.HasError() {
		t.Fatalf("knownRuleNamespaces: %v", diags)
	}

	if len(got) != 1 || got[0].role != RoleNamespaceAdmin || len(got[0].values) != 1 || got[0].values[0] != "dev" {
		t.Errorf("got %+v, want only namespace-admin with [dev]", got)
	}
}

// fakeNamespaces answers GetNamespaces and counts the calls. Calling any other API method panics.
type fakeNamespaces struct {
	platformv1connect.PlatformServiceClient

	namespaces []*platformv1.Namespace
	calls      int
}

func (f *fakeNamespaces) GetNamespaces(_ context.Context, _ *connect.Request[platformv1.GetNamespacesRequest]) (*connect.Response[platformv1.GetNamespacesResponse], error) {
	f.calls++
	return connect.NewResponse(&platformv1.GetNamespacesResponse{
		Response:   &platformv1.Response{Code: common.EnumStatusCode_OK},
		Namespaces: f.namespaces,
	}), nil
}

func TestCheckRuleNamespaces(t *testing.T) {
	ctx := context.Background()

	t.Run("names are reported with their ID", func(t *testing.T) {
		fake := &fakeNamespaces{namespaces: testNamespaces}
		r := &OrganizationGroupResource{client: &api.PlatformClient{Client: fake}}
		rules := types.SetValueMust(groupRuleObjectType, []attr.Value{
			rule(t, RoleNamespaceAdmin, types.SetValueMust(types.StringType, []attr.Value{types.StringValue("dev")})),
		})

		diags := r.checkRuleNamespaces(ctx, rules)

		if !diags.HasError() || !strings.Contains(diags[0].Detail(), devID) {
			t.Errorf("want an error mentioning %s, got %v", devID, summaries(diags))
		}
	})

	t.Run("no namespaces: Cosmo isn't called", func(t *testing.T) {
		fake := &fakeNamespaces{namespaces: testNamespaces}
		r := &OrganizationGroupResource{client: &api.PlatformClient{Client: fake}}
		rules := types.SetValueMust(groupRuleObjectType, []attr.Value{
			rule(t, RoleOrganizationViewer, types.SetNull(types.StringType)),
		})

		if diags := r.checkRuleNamespaces(ctx, rules); diags.HasError() {
			t.Errorf("unexpected diagnostics: %v", summaries(diags))
		}
		if fake.calls != 0 {
			t.Errorf("GetNamespaces called %d times, want 0", fake.calls)
		}
	})
}
