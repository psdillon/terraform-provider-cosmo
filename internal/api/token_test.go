package api_test

import (
	"context"
	"testing"

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
