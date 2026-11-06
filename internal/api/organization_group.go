package api

import (
	"context"
	"fmt"

	"connectrpc.com/connect"

	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/common"
	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
)

// OrganizationGroupRule is a role granted by an organization group, optionally
// scoped to namespace IDs or resource (target) IDs.
type OrganizationGroupRule struct {
	Role       string
	Namespaces []string
	Resources  []string
}

func (p *PlatformClient) CreateOrganizationGroup(ctx context.Context, name, description string) (*platformv1.OrganizationGroup, *ApiError) {
	request := connect.NewRequest(&platformv1.CreateOrganizationGroupRequest{
		Name:        name,
		Description: description,
	})

	response, err := p.Client.CreateOrganizationGroup(ctx, request)
	if err != nil {
		return nil, &ApiError{Err: err, Reason: "CreateOrganizationGroup", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return nil, &ApiError{Err: ErrEmptyMsg, Reason: "CreateOrganizationGroup", Status: common.EnumStatusCode_ERR}
	}

	apiError := handleErrorCodes(response.Msg.GetResponse().GetCode(), response.Msg.GetResponse().GetDetails())
	if apiError != nil {
		return nil, apiError
	}

	if response.Msg.GetGroup() == nil {
		return nil, &ApiError{Err: ErrEmptyMsg, Reason: "CreateOrganizationGroup: no group returned", Status: common.EnumStatusCode_ERR}
	}

	return response.Msg.GetGroup(), nil
}

// UpdateOrganizationGroup replaces the description and the full set of rules of a group.
func (p *PlatformClient) UpdateOrganizationGroup(ctx context.Context, groupId, description string, rules []OrganizationGroupRule) *ApiError {
	requestRules := make([]*platformv1.UpdateOrganizationGroupRequest_GroupRule, 0, len(rules))
	for _, rule := range rules {
		requestRules = append(requestRules, &platformv1.UpdateOrganizationGroupRequest_GroupRule{
			Role:       rule.Role,
			Namespaces: rule.Namespaces,
			Resources:  rule.Resources,
		})
	}

	request := connect.NewRequest(&platformv1.UpdateOrganizationGroupRequest{
		GroupId:     groupId,
		Description: description,
		Rules:       requestRules,
	})

	response, err := p.Client.UpdateOrganizationGroup(ctx, request)
	if err != nil {
		return &ApiError{Err: err, Reason: "UpdateOrganizationGroup", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return &ApiError{Err: ErrEmptyMsg, Reason: "UpdateOrganizationGroup", Status: common.EnumStatusCode_ERR}
	}

	return handleErrorCodes(response.Msg.GetResponse().GetCode(), response.Msg.GetResponse().GetDetails())
}

func (p *PlatformClient) GetOrganizationGroups(ctx context.Context) ([]*platformv1.OrganizationGroup, *ApiError) {
	request := connect.NewRequest(&platformv1.GetOrganizationGroupsRequest{})

	response, err := p.Client.GetOrganizationGroups(ctx, request)
	if err != nil {
		return nil, &ApiError{Err: err, Reason: "GetOrganizationGroups", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return nil, &ApiError{Err: ErrEmptyMsg, Reason: "GetOrganizationGroups", Status: common.EnumStatusCode_ERR}
	}

	apiError := handleErrorCodes(response.Msg.GetResponse().GetCode(), response.Msg.GetResponse().GetDetails())
	if apiError != nil {
		return nil, apiError
	}

	return response.Msg.GetGroups(), nil
}

// GetOrganizationGroup looks up a group by ID, or by name when id is empty.
// The API has no single-group lookup, so this filters the list of groups.
func (p *PlatformClient) GetOrganizationGroup(ctx context.Context, id, name string) (*platformv1.OrganizationGroup, *ApiError) {
	groups, apiError := p.GetOrganizationGroups(ctx)
	if apiError != nil {
		return nil, apiError
	}

	for _, group := range groups {
		if id != "" && group.GetGroupId() == id {
			return group, nil
		}
		if id == "" && group.GetName() == name {
			return group, nil
		}
	}

	return nil, &ApiError{Err: ErrNotFound, Reason: fmt.Sprintf("GetOrganizationGroup: group %q not found", firstNonEmpty(id, name)), Status: common.EnumStatusCode_ERR_NOT_FOUND}
}

// DeleteOrganizationGroup deletes a group. If the group still has members, API keys
// or OIDC mappers, the control plane requires toGroupId to move them to.
func (p *PlatformClient) DeleteOrganizationGroup(ctx context.Context, groupId string, toGroupId *string) *ApiError {
	request := connect.NewRequest(&platformv1.DeleteOrganizationGroupRequest{
		GroupId:   groupId,
		ToGroupId: toGroupId,
	})

	response, err := p.Client.DeleteOrganizationGroup(ctx, request)
	if err != nil {
		return &ApiError{Err: err, Reason: "DeleteOrganizationGroup", Status: common.EnumStatusCode_ERR}
	}

	if response.Msg == nil {
		return &ApiError{Err: ErrEmptyMsg, Reason: "DeleteOrganizationGroup", Status: common.EnumStatusCode_ERR}
	}

	return handleErrorCodes(response.Msg.GetResponse().GetCode(), response.Msg.GetResponse().GetDetails())
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
