package connector

import (
	"context"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

type connectedApplicationBuilder struct {
	client *client.SalesforceClient
}

func (o *connectedApplicationBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return resourceTypeConnectedApplication
}

func connectedApplicationResource(_ context.Context, app *client.ConnectedApplication) (*v2.Resource, error) {
	newAppResource, err := rs.NewResource(
		app.Name,
		resourceTypeConnectedApplication,
		app.ID,
		rs.WithAppTrait(),
	)
	if err != nil {
		return nil, err
	}

	return newAppResource, nil
}

func (o *connectedApplicationBuilder) List(
	ctx context.Context,
	parentResourceID *v2.ResourceId,
	attrs rs.SyncOpAttrs,
) (
	[]*v2.Resource,
	*rs.SyncOpResults,
	error,
) {
	token := &attrs.PageToken
	applications, nextToken, ratelimitData, err := o.client.GetConnectedApplications(
		ctx,
		token.Token,
		token.Size,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, err
	}

	rv := make([]*v2.Resource, 0, len(applications))
	for _, application := range applications {
		newResource, err := connectedApplicationResource(ctx, application)
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

func (o *connectedApplicationBuilder) Entitlements(
	ctx context.Context,
	resource *v2.Resource,
	_ rs.SyncOpAttrs,
) (
	[]*v2.Entitlement,
	*rs.SyncOpResults,
	error,
) {
	return nil, nil, nil
}

func (o *connectedApplicationBuilder) Grants(
	ctx context.Context,
	resource *v2.Resource,
	attrs rs.SyncOpAttrs,
) (
	[]*v2.Grant,
	*rs.SyncOpResults,
	error,
) {
	return nil, nil, nil
}

func newConnectedApplicationBuilder(client *client.SalesforceClient) *connectedApplicationBuilder {
	return &connectedApplicationBuilder{
		client: client,
	}
}
