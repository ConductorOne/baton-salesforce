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
	roleAssignmentEntitlementName = "assigned"
)

type roleBuilder struct {
	resourceType *v2.ResourceType
	client       *client.SalesforceClient
}

// roleResource convert a SalesforceRole into a Resource.
func roleResource(ctx context.Context, role *client.SalesforceRole) (*v2.Resource, error) {
	newRoleResource, err := resource.NewRoleResource(
		role.Name,
		resourceTypeRole,
		role.ID,
		[]resource.RoleTraitOption{},
	)
	if err != nil {
		return nil, err
	}

	return newRoleResource, nil
}

func (o *roleBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return resourceTypeRole
}

func (o *roleBuilder) List(
	ctx context.Context,
	parentResourceID *v2.ResourceId,
	pToken *pagination.Token,
) (
	[]*v2.Resource,
	string,
	annotations.Annotations,
	error,
) {
	roles, nextToken, ratelimitData, err := o.client.GetUserRoles(
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
		newResource, err := roleResource(ctx, role)
		if err != nil {
			return nil, "", outputAnnotations, err
		}

		rv = append(rv, newResource)
	}
	return rv, nextToken, outputAnnotations, nil
}

func (o *roleBuilder) Entitlements(
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
		"Roles.Entitlements",
		zap.String("resource.DisplayName", resource.DisplayName),
		zap.String("resource.Id.Resource", resource.Id.Resource),
	)
	entitlements := []*v2.Entitlement{
		entitlement.NewAssignmentEntitlement(
			resource,
			roleAssignmentEntitlementName,
			entitlement.WithGrantableTo(resourceTypeUser),
			entitlement.WithDisplayName(
				fmt.Sprintf("%s User Role", resource.DisplayName),
			),
			entitlement.WithDescription(
				fmt.Sprintf("Has the %s role in Salesforce", resource.DisplayName),
			),
		),
	}

	return entitlements, "", nil, nil
}

type UserRoleGrant struct {
	UserID     string
	UserRoleID string
}

func (o *roleBuilder) Grants(
	ctx context.Context,
	resource *v2.Resource,
	pToken *pagination.Token,
) (
	[]*v2.Grant,
	string,
	annotations.Annotations,
	error,
) {
	assignments, nextToken, ratelimitData, err := o.client.GetRoleAssignments(
		ctx,
		resource.Id.Resource,
		pToken.Token,
		pToken.Size,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, "", outputAnnotations, err
	}

	var grants []*v2.Grant
	for _, assignment := range assignments {
		grants = append(grants, grant.NewGrant(
			resource,
			roleAssignmentEntitlementName,
			&v2.ResourceId{
				ResourceType: resourceTypeUser.Id,
				Resource:     assignment.ID,
			},
		))
	}
	return grants, nextToken, outputAnnotations, nil
}

func newRoleBuilder(client *client.SalesforceClient) *roleBuilder {
	return &roleBuilder{
		resourceType: resourceTypeRole,
		client:       client,
	}
}
