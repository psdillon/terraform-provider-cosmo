package api

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"connectrpc.com/connect"

	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/common"
	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
)

func (p *PlatformClient) GetToken(ctx context.Context, name, graphName, namespace string) (*platformv1.RouterToken, *ApiError) {
	request := connect.NewRequest(&platformv1.GetRouterTokensRequest{
		FedGraphName: graphName,
		Namespace:    namespace,
	})

	response, err := p.Client.GetRouterTokens(ctx, request)
	if err != nil {
		return nil, &ApiError{Err: err, Reason: "GetToken", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return nil, &ApiError{Err: ErrEmptyMsg, Reason: "GetToken", Status: common.EnumStatusCode_ERR}
	}

	// handleErrorCodes maps ERR_NOT_FOUND (e.g. the federated graph doesn't exist) to ErrNotFound, so
	// the resource's Read removes the token from state instead of failing.
	if apiError := handleErrorCodes(response.Msg.GetResponse().GetCode(), response.Msg.GetResponse().GetDetails()); apiError != nil {
		return nil, apiError
	}

	for _, token := range response.Msg.Tokens {
		if token.Name == name {
			return token, nil
		}
	}

	return nil, &ApiError{Err: ErrNotFound, Reason: "GetToken", Status: common.EnumStatusCode_ERR}
}

// createTokenAttempts is how many times CreateToken tries to create a token when Cosmo answers with
// an internal error.
const createTokenAttempts = 5

// createTokenRetryDelay returns how long CreateToken waits before trying again. Cosmo's tokens are
// JWTs whose only time claim is in seconds, so the wait must be over a second for the next token to
// differ. The random part spreads out tokens that failed together, so they don't collide again.
var createTokenRetryDelay = func() time.Duration {
	return 1100*time.Millisecond + rand.N(time.Second)
}

func (p *PlatformClient) CreateToken(ctx context.Context, name, graphName, namespace string) (string, *ApiError) {
	request := &platformv1.CreateFederatedGraphTokenRequest{
		GraphName: graphName,
		Namespace: namespace,
		TokenName: name,
	}

	// Cosmo signs a router token from the graph, the API key's user and the current second only, so
	// two tokens for the same graph created in the same second are identical. Cosmo then fails the
	// second one with an internal error (the token column is unique), which happens when Terraform
	// creates several tokens for one graph in parallel. Nothing is stored when that happens, so try
	// again a little later.
	var response *connect.Response[platformv1.CreateFederatedGraphTokenResponse]
	var err error
	for attempt := 1; ; attempt++ {
		response, err = p.Client.CreateFederatedGraphToken(ctx, connect.NewRequest(request))
		if err == nil || connect.CodeOf(err) != connect.CodeInternal || attempt == createTokenAttempts {
			break
		}

		timer := time.NewTimer(createTokenRetryDelay())
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", &ApiError{Err: fmt.Errorf("%w (gave up retrying: %w)", err, ctx.Err()), Reason: "CreateToken", Status: common.EnumStatusCode_ERR}
		case <-timer.C:
		}
	}
	if err != nil {
		return "", &ApiError{Err: err, Reason: "CreateToken", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return "", &ApiError{Err: ErrEmptyMsg, Reason: "CreateToken", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg.GetResponse().Code != common.EnumStatusCode_OK {
		return "", &ApiError{Err: fmt.Errorf("failed to create token: %s", response.Msg.GetResponse().GetDetails()), Reason: "CreateToken", Status: common.EnumStatusCode_ERR}
	}

	return response.Msg.Token, nil
}

func (p *PlatformClient) DeleteToken(ctx context.Context, tokenName, graphName, namespace string) *ApiError {
	request := connect.NewRequest(&platformv1.DeleteRouterTokenRequest{
		TokenName:    tokenName,
		FedGraphName: graphName,
		Namespace:    namespace,
	})

	response, err := p.Client.DeleteRouterToken(ctx, request)
	if err != nil {
		return &ApiError{Err: err, Reason: "DeleteToken", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return &ApiError{Err: ErrEmptyMsg, Reason: "DeleteToken", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg.GetResponse().Code != common.EnumStatusCode_OK {
		return &ApiError{Err: fmt.Errorf("failed to delete token: %s", response.Msg), Reason: "DeleteToken", Status: common.EnumStatusCode_ERR}
	}

	return nil
}
