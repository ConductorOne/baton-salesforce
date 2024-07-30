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
	groupMemberEntitlementName = "member"
	organizationGroupName      = "All Internal Users"
)

type groupBuilder struct {
	resourceType *v2.ResourceType
	client       *client.SalesforceClient
}

func getGroupName(group *client.SalesforceGroup) string {
	typeName := ""
	switch group.Type {
	case "RoleAndSubordinates":
		typeName = " (role and subordinates)"
	case "Role":
		typeName = " (role)"
	case "Organization":
		// Singleton, read-only group that includes all Users.
		return organizationGroupName
	}
	return fmt.Sprintf("%s%s", group.Name, typeName)
}

func groupResource(ctx context.Context, group *client.SalesforceGroup) (*v2.Resource, error) {
	displayName := getGroupName(group)

	newGroupResource, err := resource.NewGroupResource(
		displayName,
		resourceTypeGroup,
		group.ID,
		[]resource.GroupTraitOption{},
	)
	if err != nil {
		return nil, err
	}

	return newGroupResource, nil
}

func (o *groupBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return resourceTypeGroup
}

func (o *groupBuilder) List(
	ctx context.Context,
	parentResourceID *v2.ResourceId,
	pToken *pagination.Token,
) ([]*v2.Resource, string, annotations.Annotations, error) {
	groups, nextToken, ratelimitData, err := o.client.GetGroups(
		ctx,
		pToken.Token,
		pToken.Size,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, "", outputAnnotations, err
	}

	rv := make([]*v2.Resource, 0)
	for _, group := range groups {
		newResource, err := groupResource(ctx, group)
		if err != nil {
			return nil, "", outputAnnotations, err
		}

		rv = append(rv, newResource)
	}
	return rv, nextToken, outputAnnotations, nil
}

// Entitlements always returns an empty slice for users.
func (o *groupBuilder) Entitlements(
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
		"Groups.Entitlements",
		zap.String("resource.DisplayName", resource.DisplayName),
		zap.String("resource.Id.Resource", resource.Id.Resource),
	)
	entitlements := []*v2.Entitlement{
		entitlement.NewAssignmentEntitlement(
			resource,
			groupMemberEntitlementName,
			entitlement.WithGrantableTo(resourceTypeUser),
			entitlement.WithDisplayName(
				fmt.Sprintf("%s Group Member", resource.DisplayName),
			),
			entitlement.WithDescription(
				fmt.Sprintf("Is member of the %s group in Salesforce", resource.DisplayName),
			),
		),
	}

	return entitlements, "", nil, nil
}

func (o *groupBuilder) Grants(
	ctx context.Context,
	resource *v2.Resource,
	pToken *pagination.Token,
) (
	[]*v2.Grant,
	string,
	annotations.Annotations,
	error,
) {
	memberships, nextToken, ratelimitData, err := o.client.GetGroupMemberships(
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
	for _, membership := range memberships {
		var resourceType *v2.ResourceType
		if membership.IsGroup {
			resourceType = resourceTypeGroup
		} else {
			resourceType = resourceTypeUser
		}

		grants = append(grants, grant.NewGrant(
			resource,
			groupMemberEntitlementName,
			&v2.ResourceId{
				ResourceType: resourceType.Id,
				Resource:     membership.PrincipalID,
			},
		))
	}

	return grants, nextToken, outputAnnotations, nil
}

func (o *groupBuilder) Grant(
	ctx context.Context,
	principal *v2.Resource,
	entitlement *v2.Entitlement,
) (annotations.Annotations, error) {
	ratelimitData, err := o.client.AddUserToGroup(
		ctx,
		principal.Id.Resource,
		entitlement.Resource.Id.Resource,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	return outputAnnotations, err
}

func (o *groupBuilder) Revoke(
	ctx context.Context,
	grant *v2.Grant,
) (annotations.Annotations, error) {
	ratelimitData, err := o.client.RemoveUserFromGroup(
		ctx,
		grant.Principal.Id.Resource,
		grant.Entitlement.Resource.Id.Resource,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	return outputAnnotations, err
}

func newGroupBuilder(client *client.SalesforceClient) *groupBuilder {
	return &groupBuilder{
		resourceType: resourceTypeGroup,
		client:       client,
	}
}
