package api

import (
	"context"

	"connectrpc.com/connect"

	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/common"
	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
)

func (p *PlatformClient) CreateNamespace(ctx context.Context, name string) *ApiError {
	request := connect.NewRequest(&platformv1.CreateNamespaceRequest{Name: name})
	response, err := p.Client.CreateNamespace(ctx, request)
	if err != nil {
		return &ApiError{Err: err, Reason: "CreateSubgraph", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return &ApiError{Err: ErrEmptyMsg, Reason: "CreateNamespace", Status: common.EnumStatusCode_ERR}
	}

	apiError := handleErrorCodes(response.Msg.GetResponse().Code, response.Msg.String())
	if apiError != nil {
		return apiError
	}

	return nil
}

func (p *PlatformClient) RenameNamespace(ctx context.Context, oldName, newName string) *ApiError {
	request := connect.NewRequest(&platformv1.RenameNamespaceRequest{
		Name:    oldName,
		NewName: newName,
	})
	response, err := p.Client.RenameNamespace(ctx, request)
	if err != nil {
		return &ApiError{Err: err, Reason: "RenameNamespace", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return &ApiError{Err: ErrEmptyMsg, Reason: "RenameNamespace", Status: common.EnumStatusCode_ERR}
	}

	apiError := handleErrorCodes(response.Msg.GetResponse().Code, response.Msg.String())
	if apiError != nil {
		return apiError
	}

	return nil
}

func (p *PlatformClient) DeleteNamespace(ctx context.Context, name string) error {
	request := connect.NewRequest(&platformv1.DeleteNamespaceRequest{Name: name})
	response, err := p.Client.DeleteNamespace(ctx, request)
	if err != nil {
		return &ApiError{Err: err, Reason: "DeleteNamespace", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return &ApiError{Err: ErrEmptyMsg, Reason: "DeleteNamespace", Status: common.EnumStatusCode_ERR}
	}

	apiError := handleErrorCodes(response.Msg.GetResponse().Code, response.Msg.String())
	if apiError != nil {
		return apiError
	}

	return nil
}

func (p *PlatformClient) GetNamespace(ctx context.Context, id, name string) (*platformv1.Namespace, *ApiError) {
	request := connect.NewRequest(&platformv1.GetNamespaceRequest{
		Name: name,
		Id:   id,
	})
	response, err := p.Client.GetNamespace(ctx, request)
	if err != nil {
		return nil, &ApiError{Err: err, Reason: "GetNamespace", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return nil, &ApiError{Err: ErrEmptyMsg, Reason: "GetNamespace", Status: common.EnumStatusCode_ERR}
	}

	apiError := handleErrorCodes(response.Msg.GetResponse().Code, response.Msg.String())
	if apiError != nil {
		return nil, apiError
	}

	return response.Msg.Namespace, nil
}

// NamespaceProposalConfig is the proposal setting of a namespace as Cosmo reports it.
type NamespaceProposalConfig struct {
	Enabled              bool
	CheckSeverityLevel   platformv1.LintSeverity
	PublishSeverityLevel platformv1.LintSeverity
}

// EnableProposalsForNamespace turns proposals on or off for a namespace.
// Turning them on resets both severity levels to "error"; turning them off
// deletes the namespace's proposal settings.
func (p *PlatformClient) EnableProposalsForNamespace(ctx context.Context, namespace string, enable bool) *ApiError {
	request := connect.NewRequest(&platformv1.EnableProposalsForNamespaceRequest{
		Namespace:       namespace,
		EnableProposals: enable,
	})
	response, err := p.Client.EnableProposalsForNamespace(ctx, request)
	if err != nil {
		return &ApiError{Err: err, Reason: "EnableProposalsForNamespace", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return &ApiError{Err: ErrEmptyMsg, Reason: "EnableProposalsForNamespace", Status: common.EnumStatusCode_ERR}
	}

	return handleErrorCodes(response.Msg.GetResponse().Code, response.Msg.String())
}

// ConfigureNamespaceProposalConfig sets the severity levels of a namespace's proposal checks.
func (p *PlatformClient) ConfigureNamespaceProposalConfig(ctx context.Context, namespace string, checkSeverity, publishSeverity platformv1.LintSeverity) *ApiError {
	request := connect.NewRequest(&platformv1.ConfigureNamespaceProposalConfigRequest{
		Namespace:            namespace,
		CheckSeverityLevel:   checkSeverity,
		PublishSeverityLevel: publishSeverity,
	})
	response, err := p.Client.ConfigureNamespaceProposalConfig(ctx, request)
	if err != nil {
		return &ApiError{Err: err, Reason: "ConfigureNamespaceProposalConfig", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return &ApiError{Err: ErrEmptyMsg, Reason: "ConfigureNamespaceProposalConfig", Status: common.EnumStatusCode_ERR}
	}

	return handleErrorCodes(response.Msg.GetResponse().Code, response.Msg.String())
}

// GetNamespaceProposalConfig returns whether proposals are enabled for a namespace and,
// if so, their severity levels. Cosmo answers ERR_UPGRADE_PLAN (see IsUpgradePlanError)
// when the organization's plan does not include proposals.
func (p *PlatformClient) GetNamespaceProposalConfig(ctx context.Context, namespace string) (*NamespaceProposalConfig, *ApiError) {
	request := connect.NewRequest(&platformv1.GetNamespaceProposalConfigRequest{Namespace: namespace})
	response, err := p.Client.GetNamespaceProposalConfig(ctx, request)
	if err != nil {
		return nil, &ApiError{Err: err, Reason: "GetNamespaceProposalConfig", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return nil, &ApiError{Err: ErrEmptyMsg, Reason: "GetNamespaceProposalConfig", Status: common.EnumStatusCode_ERR}
	}

	apiError := handleErrorCodes(response.Msg.GetResponse().Code, response.Msg.String())
	if apiError != nil {
		return nil, apiError
	}

	return &NamespaceProposalConfig{
		Enabled:              response.Msg.GetEnabled(),
		CheckSeverityLevel:   response.Msg.GetCheckSeverityLevel(),
		PublishSeverityLevel: response.Msg.GetPublishSeverityLevel(),
	}, nil
}
