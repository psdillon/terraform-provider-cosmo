package api_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/common"
	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1/platformv1connect"

	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/api"
)

// fakeRouterTokens answers GetRouterTokens with a fixed status and token list. Calling any other API
// method panics.
type fakeRouterTokens struct {
	platformv1connect.PlatformServiceClient

	code    common.EnumStatusCode
	details string
	tokens  []*platformv1.RouterToken
}

func (f *fakeRouterTokens) GetRouterTokens(_ context.Context, _ *connect.Request[platformv1.GetRouterTokensRequest]) (*connect.Response[platformv1.GetRouterTokensResponse], error) {
	return connect.NewResponse(&platformv1.GetRouterTokensResponse{
		Response: &platformv1.Response{Code: f.code, Details: &f.details},
		Tokens:   f.tokens,
	}), nil
}

func TestGetToken(t *testing.T) {
	tests := []struct {
		name         string
		fake         *fakeRouterTokens
		wantToken    bool
		wantNotFound bool
	}{
		{
			name: "token exists",
			fake: &fakeRouterTokens{
				code:   common.EnumStatusCode_OK,
				tokens: []*platformv1.RouterToken{{Name: "other"}, {Name: "token"}},
			},
			wantToken: true,
		},
		{
			name: "token missing",
			fake: &fakeRouterTokens{
				code:   common.EnumStatusCode_OK,
				tokens: []*platformv1.RouterToken{{Name: "other"}},
			},
			wantNotFound: true,
		},
		{
			name: "federated graph missing",
			fake: &fakeRouterTokens{
				code:    common.EnumStatusCode_ERR_NOT_FOUND,
				details: "Federated graph 'graph' not found",
			},
			wantNotFound: true,
		},
		{
			name: "other error",
			fake: &fakeRouterTokens{
				code:    common.EnumStatusCode_ERR,
				details: "something went wrong",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &api.PlatformClient{Client: tt.fake}

			token, apiError := client.GetToken(context.Background(), "token", "graph", "namespace")

			if tt.wantToken {
				if apiError != nil {
					t.Fatalf("expected the token, got error: %v", apiError)
				}
				if token.GetName() != "token" {
					t.Fatalf("expected token %q, got %q", "token", token.GetName())
				}
				return
			}

			if apiError == nil {
				t.Fatalf("expected an error, got token %v", token)
			}
			if got := api.IsNotFoundError(apiError); got != tt.wantNotFound {
				t.Fatalf("IsNotFoundError = %v, want %v (error: %v)", got, tt.wantNotFound, apiError)
			}
		})
	}
}

// fakeCreateToken answers CreateFederatedGraphToken with errs, one per call, and then with token.
// Calling any other API method panics.
type fakeCreateToken struct {
	platformv1connect.PlatformServiceClient

	errs  []error
	calls int
}

func (f *fakeCreateToken) CreateFederatedGraphToken(ctx context.Context, _ *connect.Request[platformv1.CreateFederatedGraphTokenRequest]) (*connect.Response[platformv1.CreateFederatedGraphTokenResponse], error) {
	f.calls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.calls <= len(f.errs) {
		return nil, f.errs[f.calls-1]
	}
	return connect.NewResponse(&platformv1.CreateFederatedGraphTokenResponse{
		Response: &platformv1.Response{Code: common.EnumStatusCode_OK},
		Token:    "token-value",
	}), nil
}

func TestCreateToken(t *testing.T) {
	defer api.SetCreateTokenRetryDelay(time.Millisecond)()

	internal := connect.NewError(connect.CodeInternal, errors.New("internal error"))
	unavailable := connect.NewError(connect.CodeUnavailable, errors.New("unavailable"))

	tests := []struct {
		name      string
		errs      []error
		wantToken bool
		wantCalls int
	}{
		{name: "created first time", wantToken: true, wantCalls: 1},
		{name: "internal error once, then created", errs: []error{internal}, wantToken: true, wantCalls: 2},
		{name: "internal error 4 times, then created", errs: []error{internal, internal, internal, internal}, wantToken: true, wantCalls: 5},
		{name: "internal error every time", errs: []error{internal, internal, internal, internal, internal}, wantCalls: 5},
		{name: "other error isn't retried", errs: []error{unavailable}, wantCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeCreateToken{errs: tt.errs}
			client := &api.PlatformClient{Client: fake}

			token, apiError := client.CreateToken(context.Background(), "token", "graph", "namespace")

			if fake.calls != tt.wantCalls {
				t.Errorf("CreateFederatedGraphToken called %d times, want %d", fake.calls, tt.wantCalls)
			}
			if tt.wantToken {
				if apiError != nil {
					t.Fatalf("expected the token, got error: %v", apiError)
				}
				if token != "token-value" {
					t.Fatalf("expected token %q, got %q", "token-value", token)
				}
				return
			}
			if apiError == nil {
				t.Fatalf("expected an error, got token %q", token)
			}
			if !errors.Is(apiError.Err, tt.errs[len(tt.errs)-1]) {
				t.Fatalf("expected the last error from Cosmo, got %v", apiError)
			}
		})
	}
}

func TestCreateTokenStopsRetryingWhenCancelled(t *testing.T) {
	defer api.SetCreateTokenRetryDelay(time.Hour)()

	internal := connect.NewError(connect.CodeInternal, errors.New("internal error"))
	fake := &fakeCreateToken{errs: []error{internal}}
	client := &api.PlatformClient{Client: fake}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, apiError := client.CreateToken(ctx, "token", "graph", "namespace")

	if apiError == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(apiError.Err, internal) || !errors.Is(apiError.Err, context.DeadlineExceeded) {
		t.Fatalf("expected Cosmo's error and the context error, got %v", apiError)
	}
	if fake.calls != 1 {
		t.Fatalf("CreateFederatedGraphToken called %d times, want 1", fake.calls)
	}
}
