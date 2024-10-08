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
	profileAssignmentEntitlementName = "assigned"
)

type profileBuilder struct {
	resourceType *v2.ResourceType
	client       *client.SalesforceClient
}

// profileResource convert a Salesforce profile into a Resource.
func profileResource(
	ctx context.Context,
	profile *client.SalesforceProfile,
) (*v2.Resource, error) {
	newProfileResource, err := resource.NewResource(
		profile.Name,
		resourceTypeProfile,
		profile.ID,
	)
	if err != nil {
		return nil, err
	}

	return newProfileResource, nil
}

func (o *profileBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return resourceTypeProfile
}

func (o *profileBuilder) List(
	ctx context.Context,
	parentResourceID *v2.ResourceId,
	pToken *pagination.Token,
) ([]*v2.Resource, string, annotations.Annotations, error) {
	profiles, nextToken, ratelimitData, err := o.client.GetProfiles(
		ctx,
		pToken.Token,
		pToken.Size,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, "", outputAnnotations, err
	}

	rv := make([]*v2.Resource, 0)
	for _, profile := range profiles {
		newResource, err := profileResource(ctx, profile)
		if err != nil {
			return nil, "", outputAnnotations, err
		}

		rv = append(rv, newResource)
	}
	return rv, nextToken, outputAnnotations, nil
}

func (o *profileBuilder) Entitlements(
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
			profileAssignmentEntitlementName,
			entitlement.WithGrantableTo(resourceTypeUser),
			entitlement.WithDisplayName(
				fmt.Sprintf("%s Profile", resource.DisplayName),
			),
			entitlement.WithDescription(
				fmt.Sprintf("Has the %s profile in Salesforce", resource.DisplayName),
			),
		),
	}

	return entitlements, "", nil, nil
}

func (o *profileBuilder) Grants(
	ctx context.Context,
	resource *v2.Resource,
	pToken *pagination.Token,
) (
	[]*v2.Grant,
	string,
	annotations.Annotations,
	error,
) {
	assignments, nextToken, ratelimitData, err := o.client.GetProfileAssignments(
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
			profileAssignmentEntitlementName,
			&v2.ResourceId{
				ResourceType: resourceTypeUser.Id,
				Resource:     assignment.ID,
			},
		))
	}
	return grants, nextToken, outputAnnotations, nil
}

func (o *profileBuilder) Grant(
	ctx context.Context,
	principal *v2.Resource,
	entitlement *v2.Entitlement,
) (annotations.Annotations, error) {
	logger := ctxzap.Extract(ctx)
	if principal.Id.ResourceType != resourceTypeUser.Id {
		logger.Warn(
			"salesforce-connector: only users can be granted a profile",
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)
		return nil, fmt.Errorf("salesforce-connector: only users can be granted a profile")
	}

	ratelimitData, err := o.client.AddUserToProfile(
		ctx,
		principal.Id.Resource,
		entitlement.Resource.Id.Resource,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	return outputAnnotations, err
}

func (o *profileBuilder) Revoke(
	ctx context.Context,
	grant *v2.Grant,
) (annotations.Annotations, error) {
	ratelimitData, err := o.client.RemoveUserFromProfile(
		ctx,
		grant.Principal.Id.Resource,
		grant.Entitlement.Resource.Id.Resource,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	return outputAnnotations, err
}

func newProfileBuilder(client *client.SalesforceClient) *profileBuilder {
	return &profileBuilder{
		resourceType: resourceTypeProfile,
		client:       client,
	}
}
