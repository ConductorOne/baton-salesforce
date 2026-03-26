package connector

import (
	"context"
	"fmt"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

const (
	permissionSetGroupMemberEntitlementName = "member"
)

type permissionSetGroupBuilder struct {
	client *client.SalesforceClient
}

// roleResource convert a SalesforceRole into a Resource.
func permissionSetGroupResource(permissionGroup *client.PermissionSetGroup) (*v2.Resource, error) {
	newResource, err := rs.NewResource(
		permissionGroup.MasterLabel,
		resourceTypePermissionSetGroup,
		permissionGroup.ID,
	)
	if err != nil {
		return nil, err
	}

	return newResource, nil
}

func (p *permissionSetGroupBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return resourceTypePermissionSetGroup
}

func (p *permissionSetGroupBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, attrs rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	token := &attrs.PageToken
	roles, nextToken, ratelimitData, err := p.client.GetPermissionSetGroups(
		ctx,
		token.Token,
		token.Size,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, err
	}

	rv := make([]*v2.Resource, 0)
	for _, role := range roles {
		newResource, err := permissionSetGroupResource(role)
		if err != nil {
			return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, err
		}

		rv = append(rv, newResource)
	}
	return rv, &rs.SyncOpResults{
		NextPageToken: nextToken,
		Annotations:   outputAnnotations,
	}, nil
}

func (p *permissionSetGroupBuilder) Entitlements(ctx context.Context, resource *v2.Resource, attrs rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	memberEntitlement := entitlement.NewAssignmentEntitlement(
		resource,
		permissionSetGroupMemberEntitlementName,
		entitlement.WithGrantableTo(resourceTypeUser),
		entitlement.WithDisplayName(
			fmt.Sprintf("%s Permission Set Group Member", resource.DisplayName),
		),
		entitlement.WithDescription(
			fmt.Sprintf("Assigned to the %s permission set group in Salesforce", resource.DisplayName),
		),
	)

	return []*v2.Entitlement{memberEntitlement}, nil, nil
}

func (p *permissionSetGroupBuilder) Grants(ctx context.Context, resource *v2.Resource, attrs rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	token := &attrs.PageToken
	assignments, nextToken, ratelimitData, err := p.client.GetPermissionSetGroupAssignments(
		ctx,
		resource.Id.Resource,
		token.Token,
		token.Size,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, err
	}

	grants := make([]*v2.Grant, 0)
	for _, assignment := range assignments {
		grants = append(grants, grant.NewGrant(
			resource,
			permissionSetGroupMemberEntitlementName,
			&v2.ResourceId{
				ResourceType: resourceTypeUser.Id,
				Resource:     assignment.UserID,
			},
		))
	}

	return grants, &rs.SyncOpResults{
		NextPageToken: nextToken,
		Annotations:   outputAnnotations,
	}, nil
}

func newPermissionSetGroupBuilder(client *client.SalesforceClient) *permissionSetGroupBuilder {
	return &permissionSetGroupBuilder{
		client: client,
	}
}

func (p *permissionSetGroupBuilder) Grant(ctx context.Context, resource *v2.Resource, entitlement *v2.Entitlement) ([]*v2.Grant, annotations.Annotations, error) {
	if resource.Id.ResourceType != resourceTypeUser.Id {
		return nil, nil, fmt.Errorf("baton-salesforce: resource type %s is not supported", resource.Id.ResourceType)
	}

	userID := resource.Id.Resource
	permissionSetGroupID := entitlement.Resource.Id.Resource

	existing, err := p.client.GetOnePermissionSetGroupAssignment(ctx, userID, permissionSetGroupID)
	if err != nil {
		return nil, nil, err
	}

	if existing != nil {
		return nil, annotations.New(&v2.GrantAlreadyExists{}), nil
	}

	_, err = p.client.AddUserToPermissionSetGroup(ctx, userID, permissionSetGroupID)
	if err != nil {
		return nil, nil, err
	}

	userGrant := grant.NewGrant(
		entitlement.Resource,
		permissionSetGroupMemberEntitlementName,
		resource.Id,
	)

	return []*v2.Grant{userGrant}, nil, nil
}

func (p *permissionSetGroupBuilder) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	if grant.Principal.Id.ResourceType != resourceTypeUser.Id {
		return nil, fmt.Errorf("baton-salesforce: resource type %s is not supported", grant.Principal.Id.ResourceType)
	}

	userID := grant.Principal.Id.Resource
	permissionSetGroupID := grant.Entitlement.Resource.Id.Resource

	existing, err := p.client.GetOnePermissionSetGroupAssignment(ctx, userID, permissionSetGroupID)
	if err != nil {
		return nil, err
	}

	if existing == nil {
		return annotations.New(&v2.GrantAlreadyRevoked{}), nil
	}

	_, err = p.client.RemoveUserFromPermissionSetGroup(ctx, userID, permissionSetGroupID)
	if err != nil {
		return nil, err
	}

	return nil, nil
}
