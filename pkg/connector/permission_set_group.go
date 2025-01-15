package connector

import (
	"context"
	"fmt"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

const (
	permissionSetGroupAssignmentEntitlementName = "assigned"
)

type permissionSetGroupBuilder struct {
	client *client.SalesforceClient
}

// roleResource convert a SalesforceRole into a Resource.
func permissionSetGroupResource(ctx context.Context, permissionGroup *client.PermissionSetGroup) (*v2.Resource, error) {
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

func (p *permissionSetGroupBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	roles, nextToken, ratelimitData, err := p.client.GetPermissionSetGroups(
		ctx,
		pToken.Token,
		pToken.Size,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, "", outputAnnotations, err
	}

	rv := make([]*v2.Resource, 0)
	for _, role := range roles {
		newResource, err := permissionSetGroupResource(ctx, role)
		if err != nil {
			return nil, "", outputAnnotations, err
		}

		rv = append(rv, newResource)
	}
	return rv, nextToken, outputAnnotations, nil
}

func (p *permissionSetGroupBuilder) Entitlements(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	v2entitlement := entitlement.NewAssignmentEntitlement(
		resource,
		permissionSetGroupAssignmentEntitlementName,
		entitlement.WithGrantableTo(resourceTypePermissionSet),
		entitlement.WithDisplayName(
			fmt.Sprintf("%s Permission Set Group", resource.DisplayName),
		),
		entitlement.WithDescription(
			fmt.Sprintf("Has the %s permission set in Salesforce", resource.DisplayName),
		),
	)

	return []*v2.Entitlement{v2entitlement}, "", nil, nil
}

func (p *permissionSetGroupBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	assignments, nextToken, ratelimitData, err := p.client.GetPermissionSetGroupComponent(
		ctx,
		resource.Id.Resource,
		pToken.Token,
		pToken.Size,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, "", outputAnnotations, err
	}

	grants := make([]*v2.Grant, 0)
	for _, assignment := range assignments {
		id, err := rs.NewResourceID(resourceTypePermissionSet, assignment.PermissionSetID)
		if err != nil {
			return nil, "", outputAnnotations, err
		}

		grants = append(grants, grant.NewGrant(
			resource,
			permissionSetGroupAssignmentEntitlementName,
			id,
		))
	}
	return grants, nextToken, outputAnnotations, nil
}

func newPermissionSetGroupBuilder(client *client.SalesforceClient) *permissionSetGroupBuilder {
	return &permissionSetGroupBuilder{
		client: client,
	}
}
