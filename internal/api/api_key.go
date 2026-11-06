package api

import (
	"context"
	"fmt"

	"connectrpc.com/connect"

	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/common"
	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
)

const (
	ApiKeyExpiresNever      = "never"
	ApiKeyExpiresThirtyDays = "30_days"
	ApiKeyExpiresSixMonths  = "6_months"
	ApiKeyExpiresOneYear    = "1_year"
)

// The control plane caps page size at 50 and organizations at 200 keys.
const apiKeysPageSize = 50

func ResolveApiKeyExpiry(expires string) (platformv1.ExpiresAt, error) {
	switch expires {
	case ApiKeyExpiresNever, "":
		return platformv1.ExpiresAt_NEVER, nil
	case ApiKeyExpiresThirtyDays:
		return platformv1.ExpiresAt_THIRTY_DAYS, nil
	case ApiKeyExpiresSixMonths:
		return platformv1.ExpiresAt_SIX_MONTHS, nil
	case ApiKeyExpiresOneYear:
		return platformv1.ExpiresAt_ONE_YEAR, nil
	default:
		return platformv1.ExpiresAt_NEVER, fmt.Errorf("unsupported expiry %q", expires)
	}
}

// CreateAPIKey creates an organization API key and returns its secret value.
// The secret is only ever returned here.
func (p *PlatformClient) CreateAPIKey(ctx context.Context, name, groupId string, expires platformv1.ExpiresAt, permissions []string) (string, *ApiError) {
	request := connect.NewRequest(&platformv1.CreateAPIKeyRequest{
		Name:        name,
		GroupId:     groupId,
		Expires:     expires,
		Permissions: permissions,
	})

	response, err := p.Client.CreateAPIKey(ctx, request)
	if err != nil {
		return "", &ApiError{Err: err, Reason: "CreateAPIKey", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return "", &ApiError{Err: ErrEmptyMsg, Reason: "CreateAPIKey", Status: common.EnumStatusCode_ERR}
	}

	apiError := handleErrorCodes(response.Msg.GetResponse().GetCode(), response.Msg.GetResponse().GetDetails())
	if apiError != nil {
		return "", apiError
	}

	return response.Msg.GetApiKey(), nil
}

// GetAPIKey finds an API key by name. The API has no single-key lookup, so this pages through all keys.
func (p *PlatformClient) GetAPIKey(ctx context.Context, name string) (*platformv1.APIKey, *ApiError) {
	for offset := int32(0); ; offset += apiKeysPageSize {
		request := connect.NewRequest(&platformv1.GetAPIKeysRequest{
			Limit:  apiKeysPageSize,
			Offset: offset,
		})

		response, err := p.Client.GetAPIKeys(ctx, request)
		if err != nil {
			return nil, &ApiError{Err: err, Reason: "GetAPIKey", Status: common.EnumStatusCode_ERR}
		}

		if response.Msg == nil {
			return nil, &ApiError{Err: ErrEmptyMsg, Reason: "GetAPIKey", Status: common.EnumStatusCode_ERR}
		}

		apiError := handleErrorCodes(response.Msg.GetResponse().GetCode(), response.Msg.GetResponse().GetDetails())
		if apiError != nil {
			return nil, apiError
		}

		keys := response.Msg.GetApiKeys()
		for _, key := range keys {
			if key.GetName() == name {
				return key, nil
			}
		}

		if len(keys) < apiKeysPageSize || offset+int32(len(keys)) >= response.Msg.GetCount() {
			break
		}
	}

	return nil, &ApiError{Err: ErrNotFound, Reason: fmt.Sprintf("GetAPIKey: API key %q not found", name), Status: common.EnumStatusCode_ERR_NOT_FOUND}
}

// UpdateAPIKey changes the group of an API key, which is the only mutable property.
func (p *PlatformClient) UpdateAPIKey(ctx context.Context, name, groupId string) *ApiError {
	request := connect.NewRequest(&platformv1.UpdateAPIKeyRequest{
		Name:    name,
		GroupId: groupId,
	})

	response, err := p.Client.UpdateAPIKey(ctx, request)
	if err != nil {
		return &ApiError{Err: err, Reason: "UpdateAPIKey", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return &ApiError{Err: ErrEmptyMsg, Reason: "UpdateAPIKey", Status: common.EnumStatusCode_ERR}
	}

	return handleErrorCodes(response.Msg.GetResponse().GetCode(), response.Msg.GetResponse().GetDetails())
}

func (p *PlatformClient) DeleteAPIKey(ctx context.Context, name string) *ApiError {
	request := connect.NewRequest(&platformv1.DeleteAPIKeyRequest{
		Name: name,
	})

	response, err := p.Client.DeleteAPIKey(ctx, request)
	if err != nil {
		return &ApiError{Err: err, Reason: "DeleteAPIKey", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return &ApiError{Err: ErrEmptyMsg, Reason: "DeleteAPIKey", Status: common.EnumStatusCode_ERR}
	}

	return handleErrorCodes(response.Msg.GetResponse().GetCode(), response.Msg.GetResponse().GetDetails())
}
