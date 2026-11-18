package connector

import (
	"context"
	"fmt"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

type licenseBuilder struct {
	resourceType *v2.ResourceType
	client       *client.SalesforceClient
}

func licenseResource(license *client.SalesforceUserLicense) (*v2.Resource, error) {
	traitOpts := []rs.LicenseProfileTraitOption{
		rs.WithLicenseName(license.Name),
		rs.WithLicenseSeats(license.TotalLicenses, license.UsedLicenses),
	}

	licenseTrait, err := rs.NewLicenseProfileTrait(traitOpts...)
	if err != nil {
		return nil, err
	}

	return rs.NewResource(
		license.Name,
		resourceTypeLicense,
		license.ID,
		rs.WithAnnotation(
			licenseTrait,
			&v2.V1Identifier{Id: fmt.Sprintf("license:%s", license.ID)},
		),
	)
}

func (l *licenseBuilder) ResourceType(_ context.Context) *v2.ResourceType {
	return l.resourceType
}

func (l *licenseBuilder) List(
	ctx context.Context,
	_ *v2.ResourceId,
	attrs rs.SyncOpAttrs,
) ([]*v2.Resource, *rs.SyncOpResults, error) {
	token := &attrs.PageToken
	licenses, nextToken, ratelimitData, err := l.client.GetUserLicenses(
		ctx,
		token.Token,
		token.Size,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, err
	}

	rv := make([]*v2.Resource, 0, len(licenses))
	for _, license := range licenses {
		resource, err := licenseResource(license)
		if err != nil {
			return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, err
		}
		rv = append(rv, resource)
	}
	return rv, &rs.SyncOpResults{
		NextPageToken: nextToken,
		Annotations:   outputAnnotations,
	}, nil
}

func (l *licenseBuilder) Entitlements(
	_ context.Context,
	_ *v2.Resource,
	_ rs.SyncOpAttrs,
) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	return nil, nil, nil
}

func (l *licenseBuilder) Grants(
	_ context.Context,
	_ *v2.Resource,
	_ rs.SyncOpAttrs,
) ([]*v2.Grant, *rs.SyncOpResults, error) {
	return nil, nil, nil
}

func newLicenseBuilder(client *client.SalesforceClient) *licenseBuilder {
	return &licenseBuilder{
		resourceType: resourceTypeLicense,
		client:       client,
	}
}
