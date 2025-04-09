package connector

import (
	"context"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
)

type connectedApplicationBuilder struct {
	client *client.SalesforceClient
}

func (o *connectedApplicationBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return resourceTypeConnectedApplication
}

func connectedApplicationResource(ctx context.Context, app *client.ConnectedApplication) (*v2.Resource, error) {
	newAppResource, err := resource.NewResource(
		app.Name,
		resourceTypeConnectedApplication,
		app.ID,
		resource.WithAppTrait(
			resource.WithAppProfile(map[string]interface{}{
				"created_by_id":      app.CreatedById,
				"created_date":       app.CreatedDate,
				"last_modified_date": app.LastModifiedDate,
			}),
		),
	)
	if err != nil {
		return nil, err
	}

	return newAppResource, nil
}

func (o *connectedApplicationBuilder) List(
	ctx context.Context,
	parentResourceID *v2.ResourceId,
	pToken *pagination.Token,
) (
	[]*v2.Resource,
	string,
	annotations.Annotations,
	error,
) {
	applications, nextToken, ratelimitData, err := o.client.GetConnectedApplications(
		ctx,
		pToken.Token,
		pToken.Size,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, "", outputAnnotations, err
	}

	rv := make([]*v2.Resource, 0, len(applications))
	for _, application := range applications {
		newResource, err := connectedApplicationResource(ctx, application)
		if err != nil {
			return nil, "", outputAnnotations, err
		}

		rv = append(rv, newResource)
	}
	return rv, nextToken, outputAnnotations, nil
}

func (o *connectedApplicationBuilder) Entitlements(
	ctx context.Context,
	resource *v2.Resource,
	_ *pagination.Token,
) (
	[]*v2.Entitlement,
	string,
	annotations.Annotations,
	error,
) {
	return nil, "", nil, nil
}

func (o *connectedApplicationBuilder) Grants(
	ctx context.Context,
	resource *v2.Resource,
	pToken *pagination.Token,
) (
	[]*v2.Grant,
	string,
	annotations.Annotations,
	error,
) {
	return nil, "", nil, nil
}

func newConnectedApplicationBuilder(client *client.SalesforceClient) *connectedApplicationBuilder {
	return &connectedApplicationBuilder{
		client: client,
	}
}
