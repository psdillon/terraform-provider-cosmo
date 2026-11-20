package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/common"
	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1/platformv1connect"

	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/api"
)

// fakeCosmo answers the proposal, publish and delete calls the way Cosmo does: while proposals are
// on with publish severity "error", publishing and deleting fail with
// ERR_SCHEMA_MISMATCH_WITH_APPROVED_PROPOSAL, except publishing a feature subgraph. Calling any
// other API method panics.
type fakeCosmo struct {
	platformv1connect.PlatformServiceClient

	mu      sync.Mutex
	calls   []string
	getCode common.EnumStatusCode
	enabled bool
	check   platformv1.LintSeverity
	publish platformv1.LintSeverity

	// unknown is sent with the proposal settings as fields this provider doesn't know about, like
	// settings added to Cosmo later. Turning proposals on replaces it with unknownAfterEnable.
	unknown, unknownAfterEnable []byte

	// featureSubgraph makes publishes act like feature subgraph publishes, which Cosmo doesn't
	// compare with proposals.
	featureSubgraph bool

	// ignoreConfigure makes setting the severity levels answer OK without changing them.
	ignoreConfigure bool

	// Set to make the matching call fail.
	disableCode, enableCode, opCode common.EnumStatusCode

	// opHook runs during a publish or delete, before it answers.
	opHook func()
}

func ok() *platformv1.Response { return &platformv1.Response{Code: common.EnumStatusCode_OK} }

func (f *fakeCosmo) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeCosmo) GetNamespaceProposalConfig(_ context.Context, _ *connect.Request[platformv1.GetNamespaceProposalConfigRequest]) (*connect.Response[platformv1.GetNamespaceProposalConfigResponse], error) {
	f.record("get")
	f.mu.Lock()
	defer f.mu.Unlock()
	msg := &platformv1.GetNamespaceProposalConfigResponse{
		Response:             &platformv1.Response{Code: f.getCode},
		Enabled:              f.enabled,
		CheckSeverityLevel:   f.check,
		PublishSeverityLevel: f.publish,
	}
	msg.ProtoReflect().SetUnknown(f.unknown)
	return connect.NewResponse(msg), nil
}

func (f *fakeCosmo) EnableProposalsForNamespace(ctx context.Context, req *connect.Request[platformv1.EnableProposalsForNamespaceRequest]) (*connect.Response[platformv1.EnableProposalsForNamespaceResponse], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.Msg.EnableProposals {
		f.calls = append(f.calls, "enable")
		if f.enableCode != common.EnumStatusCode_OK {
			return connect.NewResponse(&platformv1.EnableProposalsForNamespaceResponse{Response: &platformv1.Response{Code: f.enableCode}}), nil
		}
		f.enabled, f.check, f.publish = true, platformv1.LintSeverity_error, platformv1.LintSeverity_error
		f.unknown = f.unknownAfterEnable
	} else {
		f.calls = append(f.calls, "disable")
		if f.disableCode != common.EnumStatusCode_OK {
			return connect.NewResponse(&platformv1.EnableProposalsForNamespaceResponse{Response: &platformv1.Response{Code: f.disableCode}}), nil
		}
		f.enabled = false
	}
	return connect.NewResponse(&platformv1.EnableProposalsForNamespaceResponse{Response: ok()}), nil
}

func (f *fakeCosmo) ConfigureNamespaceProposalConfig(ctx context.Context, req *connect.Request[platformv1.ConfigureNamespaceProposalConfigRequest]) (*connect.Response[platformv1.ConfigureNamespaceProposalConfigResponse], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "configure "+req.Msg.CheckSeverityLevel.String()+"/"+req.Msg.PublishSeverityLevel.String())
	if !f.ignoreConfigure {
		f.check, f.publish = req.Msg.CheckSeverityLevel, req.Msg.PublishSeverityLevel
	}
	return connect.NewResponse(&platformv1.ConfigureNamespaceProposalConfigResponse{Response: ok()}), nil
}

// runOp records a publish or delete and returns the status code Cosmo would answer right now.
func (f *fakeCosmo) runOp(name string) common.EnumStatusCode {
	if f.opHook != nil {
		f.opHook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
	if f.enabled && f.publish == platformv1.LintSeverity_error && !(name == "publish" && f.featureSubgraph) {
		return common.EnumStatusCode_ERR_SCHEMA_MISMATCH_WITH_APPROVED_PROPOSAL
	}
	return f.opCode
}

func (f *fakeCosmo) PublishFederatedSubgraph(_ context.Context, _ *connect.Request[platformv1.PublishFederatedSubgraphRequest]) (*connect.Response[platformv1.PublishFederatedSubgraphResponse], error) {
	code := f.runOp("publish")
	return connect.NewResponse(&platformv1.PublishFederatedSubgraphResponse{Response: &platformv1.Response{Code: code}}), nil
}

func (f *fakeCosmo) DeleteFederatedSubgraph(_ context.Context, _ *connect.Request[platformv1.DeleteFederatedSubgraphRequest]) (*connect.Response[platformv1.DeleteFederatedSubgraphResponse], error) {
	code := f.runOp("delete")
	return connect.NewResponse(&platformv1.DeleteFederatedSubgraphResponse{Response: &platformv1.Response{Code: code}}), nil
}

func onWith(check, publish platformv1.LintSeverity) *fakeCosmo {
	return &fakeCosmo{enabled: true, check: check, publish: publish}
}

// newSetting is how a proposal setting added to Cosmo later would arrive: field 5, value 1.
var newSetting = []byte{5<<3 | 0, 1}

func onWithNewSetting(check, publish platformv1.LintSeverity) *fakeCosmo {
	f := onWith(check, publish)
	f.unknown, f.unknownAfterEnable = newSetting, newSetting
	return f
}

func TestProposalsSuspendedForPublishAndDelete(t *testing.T) {
	warn, errSev := platformv1.LintSeverity_warn, platformv1.LintSeverity_error

	cases := []struct {
		name string
		fake func() *fakeCosmo
		want []string
	}{
		{"proposals off", func() *fakeCosmo { return &fakeCosmo{} }, []string{"get", "%s"}},
		{"plan without proposals", func() *fakeCosmo { return &fakeCosmo{getCode: common.EnumStatusCode_ERR_UPGRADE_PLAN} }, []string{"get", "%s"}},
		{"on with default levels", func() *fakeCosmo { return onWith(errSev, errSev) }, []string{"get", "disable", "%s", "enable", "get"}},
		{"on with custom levels", func() *fakeCosmo { return onWith(warn, errSev) }, []string{"get", "disable", "%s", "enable", "configure warn/error", "get"}},
		// Cosmo only warns, so proposals are left alone.
		{"on with publish warn", func() *fakeCosmo { return onWith(errSev, warn) }, []string{"get", "%s"}},
		{"on with publish warn and new setting", func() *fakeCosmo { return onWithNewSetting(warn, warn) }, []string{"get", "%s"}},
	}

	ops := map[string]func(c *api.PlatformClient) *api.ApiError{
		"publish": func(c *api.PlatformClient) *api.ApiError {
			_, err := c.PublishSubgraph(context.Background(), "products", "default", "type Query { a: String }")
			return err
		},
		"delete": func(c *api.PlatformClient) *api.ApiError {
			return c.DeleteSubgraph(context.Background(), "products", "default")
		},
	}

	for opName, op := range ops {
		for _, tc := range cases {
			t.Run(opName+"/"+tc.name, func(t *testing.T) {
				fake := tc.fake()
				before := [3]any{fake.enabled, fake.check, fake.publish}

				if err := op(&api.PlatformClient{Client: fake}); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				want := make([]string, len(tc.want))
				for i, c := range tc.want {
					want[i] = strings.Replace(c, "%s", opName, 1)
				}
				if !reflect.DeepEqual(fake.calls, want) {
					t.Errorf("calls = %v, want %v", fake.calls, want)
				}
				if after := [3]any{fake.enabled, fake.check, fake.publish}; after != before {
					t.Errorf("proposal setting = %v afterwards, want %v as before", after, before)
				}
			})
		}
	}
}

func TestProposalsRestoredWhenPublishFails(t *testing.T) {
	fake := onWith(platformv1.LintSeverity_warn, platformv1.LintSeverity_error)
	fake.opCode = common.EnumStatusCode_ERR_SUBGRAPH_COMPOSITION_FAILED

	_, err := (&api.PlatformClient{Client: fake}).PublishSubgraph(context.Background(), "products", "default", "")
	if err == nil || !api.IsSubgraphCompositionFailedError(err) {
		t.Fatalf("error = %v, want the publish's composition error", err)
	}

	want := []string{"get", "disable", "publish", "enable", "configure warn/error", "get"}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Errorf("calls = %v, want %v", fake.calls, want)
	}
}

func TestFeatureSubgraphPublishLeavesProposalsAlone(t *testing.T) {
	fake := onWith(platformv1.LintSeverity_error, platformv1.LintSeverity_error)
	fake.featureSubgraph = true

	if _, err := (&api.PlatformClient{Client: fake}).PublishFeatureSubgraph(context.Background(), "products-ff", "default", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"publish"}; !reflect.DeepEqual(fake.calls, want) {
		t.Errorf("calls = %v, want %v", fake.calls, want)
	}
}

func TestNothingChangedWhenCosmoHasUnknownProposalSettings(t *testing.T) {
	ops := map[string]func(c *api.PlatformClient) *api.ApiError{
		"publish": func(c *api.PlatformClient) *api.ApiError {
			_, err := c.PublishSubgraph(context.Background(), "products", "default", "")
			return err
		},
		"delete": func(c *api.PlatformClient) *api.ApiError {
			return c.DeleteSubgraph(context.Background(), "products", "default")
		},
	}

	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			fake := onWithNewSetting(platformv1.LintSeverity_warn, platformv1.LintSeverity_error)

			err := op(&api.PlatformClient{Client: fake})
			if err == nil || !errors.Is(err.Err, api.ErrUnknownProposalSettings) {
				t.Fatalf("error = %v, want ErrUnknownProposalSettings", err)
			}
			if !strings.Contains(err.Reason, "Nothing was changed") {
				t.Errorf("error %q does not say nothing was changed", err.Reason)
			}
			if want := []string{"get"}; !reflect.DeepEqual(fake.calls, want) {
				t.Errorf("calls = %v, want %v (proposals must not be touched)", fake.calls, want)
			}
		})
	}
}

func TestErrorWhenSettingsDifferAfterRestore(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *fakeCosmo)
		want  string
	}{
		{"severity not restored", func(f *fakeCosmo) { f.ignoreConfigure = true }, `check severity is "error" instead of "warn"`},
		{"new setting appears", func(f *fakeCosmo) { f.unknownAfterEnable = newSetting }, "doesn't know about"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := onWith(platformv1.LintSeverity_warn, platformv1.LintSeverity_error)
			tc.setup(fake)

			_, err := (&api.PlatformClient{Client: fake}).PublishSubgraph(context.Background(), "products", "default", "")
			if err == nil || !errors.Is(err.Err, api.ErrProposalsNotRestored) {
				t.Fatalf("error = %v, want ErrProposalsNotRestored", err)
			}
			if !strings.Contains(err.Reason, tc.want) {
				t.Errorf("error %q does not mention %q", err.Reason, tc.want)
			}
		})
	}
}

func TestProposalsRestoredWhenInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fake := onWith(platformv1.LintSeverity_warn, platformv1.LintSeverity_error)
	fake.opHook = cancel // Terraform is interrupted while the subgraph is being deleted.

	if err := (&api.PlatformClient{Client: fake}).DeleteSubgraph(ctx, "products", "default"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fake.enabled || fake.check != platformv1.LintSeverity_warn {
		t.Errorf("proposals not restored after interruption: enabled=%v check=%v", fake.enabled, fake.check)
	}
}

func TestNothingChangedWhenProposalsCannotBeTurnedOff(t *testing.T) {
	fake := onWith(platformv1.LintSeverity_error, platformv1.LintSeverity_error)
	fake.disableCode = common.EnumStatusCode_ERROR_NOT_AUTHORIZED

	err := (&api.PlatformClient{Client: fake}).DeleteSubgraph(context.Background(), "products", "default")
	if err == nil || !strings.Contains(err.Reason, "write access") {
		t.Fatalf("error = %v, want one explaining proposals could not be turned off", err)
	}

	want := []string{"get", "disable"}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Errorf("calls = %v, want %v (the subgraph must not be deleted)", fake.calls, want)
	}
}

func TestErrorWhenProposalsCannotBeTurnedBackOn(t *testing.T) {
	fake := onWith(platformv1.LintSeverity_warn, platformv1.LintSeverity_error)
	fake.enableCode = common.EnumStatusCode_ERR

	_, err := (&api.PlatformClient{Client: fake}).PublishSubgraph(context.Background(), "products", "default", "")
	if err == nil || !errors.Is(err.Err, api.ErrProposalsNotRestored) {
		t.Fatalf("error = %v, want ErrProposalsNotRestored", err)
	}
	for _, s := range []string{`"default"`, `check severity "warn"`, `publish severity "error"`} {
		if !strings.Contains(err.Reason, s) {
			t.Errorf("error %q does not mention %s", err.Reason, s)
		}
	}
}

// Terraform publishes several subgraphs at once. None of them may run while another has already
// turned proposals back on, and proposals must end up as they were.
func TestConcurrentPublishesInOneNamespace(t *testing.T) {
	fake := onWith(platformv1.LintSeverity_warn, platformv1.LintSeverity_error)
	client := &api.PlatformClient{Client: fake}

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan *api.ApiError, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.PublishSubgraph(context.Background(), "products", "default", ""); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("publish failed: %v", err)
	}
	if !fake.enabled || fake.check != platformv1.LintSeverity_warn || fake.publish != platformv1.LintSeverity_error {
		t.Errorf("proposals not restored: enabled=%v check=%v publish=%v", fake.enabled, fake.check, fake.publish)
	}
}

// unknownSettingsServer answers GetNamespaceProposalConfig over HTTP with a proposal setting this
// provider doesn't know about.
type unknownSettingsServer struct {
	platformv1connect.UnimplementedPlatformServiceHandler
}

func (unknownSettingsServer) GetNamespaceProposalConfig(_ context.Context, _ *connect.Request[platformv1.GetNamespaceProposalConfigRequest]) (*connect.Response[platformv1.GetNamespaceProposalConfigResponse], error) {
	msg := &platformv1.GetNamespaceProposalConfigResponse{Response: ok(), Enabled: true, PublishSeverityLevel: platformv1.LintSeverity_error}
	msg.ProtoReflect().SetUnknown(newSetting)
	return connect.NewResponse(msg), nil
}

// The provider must still see settings it doesn't know about after they have been sent over the
// network, the way the real client receives them.
func TestUnknownSettingsSurviveTheNetwork(t *testing.T) {
	path, handler := platformv1connect.NewPlatformServiceHandler(unknownSettingsServer{})
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := httptest.NewServer(mux)
	defer server.Close()

	client, err := api.NewClient("test-key", server.URL)
	if err != nil {
		t.Fatal(err)
	}

	config, apiErr := client.GetNamespaceProposalConfig(context.Background(), "default")
	if apiErr != nil {
		t.Fatalf("unexpected error: %v", apiErr)
	}
	if !config.HasUnknownSettings() {
		t.Error("the setting this provider doesn't know about was lost on the way")
	}

	_, apiErr = client.PublishSubgraph(context.Background(), "products", "default", "")
	if apiErr == nil || !errors.Is(apiErr.Err, api.ErrUnknownProposalSettings) {
		t.Errorf("publish error = %v, want ErrUnknownProposalSettings", apiErr)
	}
}
