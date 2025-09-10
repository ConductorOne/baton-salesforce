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
	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

const (
	permissionSetAssignmentEntitlementName = "assigned"
)

type permissionBuilder struct {
	resourceType *v2.ResourceType
	client       *client.SalesforceClient
}

func permissionResource(_ context.Context, permission *client.SalesforcePermission) (*v2.Resource, error) {
	newPermissionResource, err := resource.NewResource(
		fmt.Sprintf("%s - %s", permission.Type, permission.Name),
		resourceTypePermissionSet,
		permission.ID,
	)
	if err != nil {
		return nil, err
	}

	return newPermissionResource, nil
}

func (o *permissionBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return resourceTypePermissionSet
}

func (o *permissionBuilder) List(
	ctx context.Context,
	parentResourceID *v2.ResourceId,
	pToken *pagination.Token,
) (
	[]*v2.Resource,
	string,
	annotations.Annotations,
	error,
) {
	permissions, nextToken, ratelimitData, err := o.client.GetPermissionSets(
		ctx,
		pToken.Token,
		pToken.Size,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, "", outputAnnotations, err
	}

	rv := make([]*v2.Resource, 0)
	for _, permission := range permissions {
		newResource, err := permissionResource(ctx, permission)
		if err != nil {
			return nil, "", outputAnnotations, err
		}

		rv = append(rv, newResource)
	}
	return rv, nextToken, outputAnnotations, nil
}

func (o *permissionBuilder) Entitlements(
	ctx context.Context,
	resource *v2.Resource,
	_ *pagination.Token,
) (
	[]*v2.Entitlement,
	string,
	annotations.Annotations,
	error,
) {
	logger := ctxzap.Extract(ctx)
	logger.Debug(
		"Profiles.Entitlements",
		zap.String("resource.DisplayName", resource.DisplayName),
		zap.String("resource.Id.Resource", resource.Id.Resource),
	)
	entitlements := []*v2.Entitlement{
		entitlement.NewAssignmentEntitlement(
			resource,
			permissionSetAssignmentEntitlementName,
			entitlement.WithGrantableTo(resourceTypeUser),
			entitlement.WithDisplayName(
				fmt.Sprintf("%s Permission Set", resource.DisplayName),
			),
			entitlement.WithDescription(
				fmt.Sprintf("Has the %s permission set in Salesforce", resource.DisplayName),
			),
		),
	}

	return entitlements, "", nil, nil
}

func (o *permissionBuilder) Grants(
	ctx context.Context,
	resource *v2.Resource,
	pToken *pagination.Token,
) (
	[]*v2.Grant,
	string,
	annotations.Annotations,
	error,
) {
	assignments, nextToken, ratelimitData, err := o.client.GetPermissionSetAssignments(
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
		grants = append(grants, grant.NewGrant(
			resource,
			permissionSetAssignmentEntitlementName,
			&v2.ResourceId{
				ResourceType: resourceTypeUser.Id,
				Resource:     assignment.UserID,
			},
		))
	}
	return grants, nextToken, outputAnnotations, nil
}

func (o *permissionBuilder) Grant(
	ctx context.Context,
	principal *v2.Resource,
	entitlement *v2.Entitlement,
) (annotations.Annotations, error) {
	logger := ctxzap.Extract(ctx)
	if principal.Id.ResourceType != resourceTypeUser.Id {
		logger.Warn(
			"salesforce-connector: only users can be granted permission sets",
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)
		return nil, fmt.Errorf("salesforce-connector: only users can be granted permission sets")
	}

	ratelimitData, err := o.client.AddUserToPermissionSet(
		ctx,
		principal.Id.Resource,
		entitlement.Resource.Id.Resource,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	return outputAnnotations, err
}

func (o *permissionBuilder) Revoke(
	ctx context.Context,
	grant *v2.Grant,
) (annotations.Annotations, error) {
	ratelimitData, err := o.client.RemoveUserFromPermissionSet(
		ctx,
		grant.Principal.Id.Resource,
		grant.Entitlement.Resource.Id.Resource,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	return outputAnnotations, err
}

func newPermissionBuilder(client *client.SalesforceClient) *permissionBuilder {
	return &permissionBuilder{
		resourceType: resourceTypePermissionSet,
		client:       client,
	}
}
