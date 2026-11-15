package namespace

import (
	"context"
	"reflect"
	"testing"

	"connectrpc.com/connect"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/common"
	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1/platformv1connect"

	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/api"
)

// fakeProposalClient records the proposal calls and answers them like Cosmo does.
// Calling any other API method panics (the embedded interface is nil).
type fakeProposalClient struct {
	platformv1connect.PlatformServiceClient

	calls   []string
	getCode common.EnumStatusCode
	enabled bool
	check   platformv1.LintSeverity
	publish platformv1.LintSeverity
}

func (f *fakeProposalClient) EnableProposalsForNamespace(_ context.Context, req *connect.Request[platformv1.EnableProposalsForNamespaceRequest]) (*connect.Response[platformv1.EnableProposalsForNamespaceResponse], error) {
	if req.Msg.EnableProposals {
		f.calls = append(f.calls, "enable")
		f.enabled, f.check, f.publish = true, platformv1.LintSeverity_error, platformv1.LintSeverity_error
	} else {
		f.calls = append(f.calls, "disable")
		f.enabled = false
	}
	return connect.NewResponse(&platformv1.EnableProposalsForNamespaceResponse{
		Response: &platformv1.Response{Code: common.EnumStatusCode_OK},
	}), nil
}

func (f *fakeProposalClient) ConfigureNamespaceProposalConfig(_ context.Context, req *connect.Request[platformv1.ConfigureNamespaceProposalConfigRequest]) (*connect.Response[platformv1.ConfigureNamespaceProposalConfigResponse], error) {
	f.calls = append(f.calls, "configure "+req.Msg.CheckSeverityLevel.String()+"/"+req.Msg.PublishSeverityLevel.String())
	f.check, f.publish = req.Msg.CheckSeverityLevel, req.Msg.PublishSeverityLevel
	return connect.NewResponse(&platformv1.ConfigureNamespaceProposalConfigResponse{
		Response: &platformv1.Response{Code: common.EnumStatusCode_OK},
	}), nil
}

func (f *fakeProposalClient) GetNamespaceProposalConfig(_ context.Context, _ *connect.Request[platformv1.GetNamespaceProposalConfigRequest]) (*connect.Response[platformv1.GetNamespaceProposalConfigResponse], error) {
	return connect.NewResponse(&platformv1.GetNamespaceProposalConfigResponse{
		Response:             &platformv1.Response{Code: f.getCode},
		Enabled:              f.enabled,
		CheckSeverityLevel:   f.check,
		PublishSeverityLevel: f.publish,
	}), nil
}

func proposals(check, publish string) *NamespaceProposalsModel {
	return &NamespaceProposalsModel{
		CheckSeverityLevel:   types.StringValue(check),
		PublishSeverityLevel: types.StringValue(publish),
	}
}

func TestApplyProposals(t *testing.T) {
	cases := []struct {
		name             string
		current, desired *NamespaceProposalsModel
		want             []string
	}{
		{"stays off", nil, nil, nil},
		{"turn on", nil, proposals("warn", "error"), []string{"enable", "configure warn/error"}},
		{"change levels", proposals("error", "error"), proposals("error", "warn"), []string{"configure error/warn"}},
		{"turn off", proposals("warn", "warn"), nil, []string{"disable"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeProposalClient{}
			r := &NamespaceResource{client: &api.PlatformClient{Client: fake}}

			if err := r.applyProposals(context.Background(), "ns", tc.current, tc.desired); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(fake.calls, tc.want) {
				t.Fatalf("calls = %v, want %v", fake.calls, tc.want)
			}
		})
	}
}

func TestReadProposals(t *testing.T) {
	cases := []struct {
		name    string
		fake    fakeProposalClient
		want    *NamespaceProposalsModel
		wantErr bool
	}{
		{"off", fakeProposalClient{getCode: common.EnumStatusCode_OK}, nil, false},
		{
			"on",
			fakeProposalClient{getCode: common.EnumStatusCode_OK, enabled: true, check: platformv1.LintSeverity_warn, publish: platformv1.LintSeverity_error},
			proposals("warn", "error"),
			false,
		},
		// Organizations whose plan has no proposals must still be able to manage namespaces.
		{"plan without proposals", fakeProposalClient{getCode: common.EnumStatusCode_ERR_UPGRADE_PLAN}, nil, false},
		{"other error", fakeProposalClient{getCode: common.EnumStatusCode_ERR}, nil, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &NamespaceResource{client: &api.PlatformClient{Client: &tc.fake}}

			got, err := r.readProposals(context.Background(), "ns")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestNamespaceSchemaIsValid(t *testing.T) {
	resp := &resource.SchemaResponse{}
	NewNamespaceResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	if diags := resp.Schema.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatalf("invalid schema: %v", diags)
	}
}
