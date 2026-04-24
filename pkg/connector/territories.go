package connector

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/simpleforce"
)

const (
	territoryMemberPermission     = "member"
	territoryRolePermissionPrefix = "role:"
)

type territoryBuilder struct {
	client *client.SalesforceClient
}

func (t *territoryBuilder) ResourceType(_ context.Context) *v2.ResourceType {
	return resourceTypeTerritory
}

func (t *territoryBuilder) List(ctx context.Context, _ *v2.ResourceId, attrs rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	territories, nextToken, ratelimitData, err := t.client.GetTerritories(ctx, attrs.PageToken.Token, attrs.PageToken.Size)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, err
	}

	resources := make([]*v2.Resource, 0, len(territories))
	for _, territory := range territories {
		resource, err := territoryResource(territory)
		if err != nil {
			return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, err
		}
		resources = append(resources, resource)
	}

	return resources, &rs.SyncOpResults{
		NextPageToken: nextToken,
		Annotations:   outputAnnotations,
	}, nil
}

func (t *territoryBuilder) Entitlements(_ context.Context, _ *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	return nil, &rs.SyncOpResults{}, nil
}

func (t *territoryBuilder) StaticEntitlements(ctx context.Context, _ rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	roles, ratelimitData, err := t.client.GetTerritoryRoles(ctx)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, fmt.Errorf("baton-salesforce: territories: failed to get static entitlements: %w", err)
	}

	ents := make([]*v2.Entitlement, 0, 1+len(roles))
	ents = append(ents, entitlement.NewAssignmentEntitlement(
		nil,
		territoryMemberPermission,
		entitlement.WithDisplayName("Member"),
		entitlement.WithDescription("Assigned to this territory in Salesforce"),
		entitlement.WithGrantableTo(resourceTypeUser),
	))

	for _, role := range roles {
		slug := fmt.Sprintf("%s%s", territoryRolePermissionPrefix, role)
		ents = append(ents, entitlement.NewAssignmentEntitlement(
			nil,
			slug,
			entitlement.WithDisplayName(role),
			entitlement.WithDescription(fmt.Sprintf("Has the %s role in this territory in Salesforce", role)),
			entitlement.WithGrantableTo(resourceTypeUser),
		))
	}

	return ents, &rs.SyncOpResults{Annotations: outputAnnotations}, nil
}

func (t *territoryBuilder) Grants(ctx context.Context, resource *v2.Resource, attrs rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	members, nextToken, ratelimitData, err := t.client.GetTerritoryMembers(ctx, resource.Id.Resource, attrs.PageToken.Token, attrs.PageToken.Size)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, err
	}

	grants := make([]*v2.Grant, 0, 2*len(members))
	for _, member := range members {
		principalID := &v2.ResourceId{
			ResourceType: resourceTypeUser.Id,
			Resource:     member.UserId,
		}
		grants = append(grants, grant.NewGrant(resource, territoryMemberPermission, principalID))
		if member.RoleInTerritory2 != "" {
			grants = append(grants, grant.NewGrant(resource, fmt.Sprintf("%s%s", territoryRolePermissionPrefix, member.RoleInTerritory2), principalID))
		}
	}

	return grants, &rs.SyncOpResults{
		NextPageToken: nextToken,
		Annotations:   outputAnnotations,
	}, nil
}

func (t *territoryBuilder) Grant(
	ctx context.Context,
	principal *v2.Resource,
	ent *v2.Entitlement,
) ([]*v2.Grant, annotations.Annotations, error) {
	if principal.Id.ResourceType != resourceTypeUser.Id {
		return nil, nil, fmt.Errorf("baton-salesforce: only users can be granted territory membership")
	}

	userID := principal.Id.Resource
	territoryID := ent.Resource.Id.Resource
	permission, err := parseTerritoryEntitlementID(ent.Id)
	if err != nil {
		return nil, nil, err
	}

	// membership is nil when the user is not a member of the territory (ErrObjectNotFound).
	membership, ratelimitData, err := t.client.GetUserTerritoryAssociation(ctx, userID, territoryID)
	if err != nil && !errors.Is(err, client.ErrObjectNotFound) {
		return nil, client.WithRateLimitAnnotations(ratelimitData), err
	}

	if permission == territoryMemberPermission {
		// User is already a member: nothing to do.
		if membership != nil {
			return nil, annotations.New(&v2.GrantAlreadyExists{}), nil
		}
		ratelimitData, err = t.client.AddUserToTerritory(ctx, userID, territoryID)
		outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
		if err != nil {
			if errors.Is(err, client.ErrObjectAlreadyExists) {
				return nil, annotations.New(&v2.GrantAlreadyExists{}), nil
			}
			return nil, outputAnnotations, err
		}
		return []*v2.Grant{grant.NewGrant(ent.Resource, territoryMemberPermission, principal.Id)}, outputAnnotations, nil
	}

	role := strings.TrimPrefix(permission, territoryRolePermissionPrefix)

	if membership == nil {
		// User is not a member yet: create the association with the role. Returns both
		// the member grant and the role grant since membership is a prerequisite.
		ratelimitData, err = t.client.AddUserToTerritoryWithRole(ctx, userID, territoryID, role)
		outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
		if err != nil {
			if errors.Is(err, client.ErrObjectAlreadyExists) {
				return nil, annotations.New(&v2.GrantAlreadyExists{}), nil
			}
			return nil, outputAnnotations, err
		}
		memberGrant := grant.NewGrant(ent.Resource, territoryMemberPermission, principal.Id)
		roleGrant := grant.NewGrant(ent.Resource, fmt.Sprintf("%s%s", territoryRolePermissionPrefix, role), principal.Id)
		return []*v2.Grant{memberGrant, roleGrant}, outputAnnotations, nil
	}

	// User is already a member: update the role on the existing association record.
	if membership.StringField("RoleInTerritory2") == role {
		return nil, annotations.New(&v2.GrantAlreadyExists{}), nil
	}

	ratelimitData, err = t.client.SetUserTerritoryRole(ctx, membership.ID(), role)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, outputAnnotations, err
	}
	return []*v2.Grant{grant.NewGrant(ent.Resource, fmt.Sprintf("%s%s", territoryRolePermissionPrefix, role), principal.Id)}, outputAnnotations, nil
}

func (t *territoryBuilder) Revoke(
	ctx context.Context,
	g *v2.Grant,
) (annotations.Annotations, error) {
	userID := g.Principal.Id.Resource
	territoryID := g.Entitlement.Resource.Id.Resource
	permission, err := parseTerritoryEntitlementID(g.Entitlement.Id)
	if err != nil {
		return nil, err
	}

	if permission == territoryMemberPermission {
		ratelimitData, err := t.client.RemoveUserFromTerritory(ctx, userID, territoryID)
		if err != nil {
			if errors.Is(err, client.ErrObjectNotFound) {
				return annotations.New(&v2.GrantAlreadyRevoked{}), nil
			}
			return client.WithRateLimitAnnotations(ratelimitData), err
		}
		return client.WithRateLimitAnnotations(ratelimitData), nil
	}

	ratelimitData, err := t.client.ClearUserTerritoryRole(ctx, userID, territoryID)
	if err != nil {
		if errors.Is(err, client.ErrObjectNotFound) || errors.Is(err, client.ErrRoleAlreadyCleared) {
			return annotations.New(&v2.GrantAlreadyRevoked{}), nil
		}
		return client.WithRateLimitAnnotations(ratelimitData), err
	}
	return client.WithRateLimitAnnotations(ratelimitData), nil
}

func territoryResource(record simpleforce.SObject) (*v2.Resource, error) {
	var opts []rs.ResourceOption
	if parentID := record.StringField("ParentTerritory2Id"); parentID != "" {
		opts = append(opts, rs.WithParentResourceID(&v2.ResourceId{
			ResourceType: resourceTypeTerritory.Id,
			Resource:     parentID,
		}))
	}

	return rs.NewResource(
		record.StringField("Name"),
		resourceTypeTerritory,
		record.ID(),
		opts...,
	)
}

func newTerritoryBuilder(c *client.SalesforceClient) *territoryBuilder {
	return &territoryBuilder{client: c}
}

func parseTerritoryEntitlementID(eID string) (string, error) {
	parts := strings.SplitN(eID, ":", 3)
	if len(parts) != 3 {
		return "", fmt.Errorf("baton-salesforce: unexpected territory entitlement ID: %s", eID)
	}
	return parts[2], nil
}
