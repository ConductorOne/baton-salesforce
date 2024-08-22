package connector

import (
	"context"
	"fmt"
	"io"
	"net/url"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

func annotationsForUserResourceType() annotations.Annotations {
	annos := annotations.Annotations{}
	annos.Update(&v2.SkipEntitlementsAndGrants{})
	return annos
}

type Salesforce struct {
	client                    *client.SalesforceClient
	ctx                       context.Context
	instanceURL               string
	shouldUseUsernameForEmail bool
}

// fallBackToHTTPS checks to domain and tacks on "https://" if no scheme is
// specified. This exists so that a user can override the scheme by including it
// in the passed "domain-url" config.
func fallBackToHTTPS(domain string) (string, error) {
	parsed, err := url.Parse(domain)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" {
		parsed, err = url.Parse(fmt.Sprintf("https://%s", domain))
		if err != nil {
			return "", err
		}
	}
	return parsed.String(), nil
}

// ResourceSyncers returns a ResourceSyncer for each resource type that should
// be synced from the upstream service.
func (d *Salesforce) ResourceSyncers(ctx context.Context) []connectorbuilder.ResourceSyncer {
	return []connectorbuilder.ResourceSyncer{
		newUserBuilder(d.client, d.shouldUseUsernameForEmail),
		newGroupBuilder(d.client),
		newPermissionBuilder(d.client),
		newProfileBuilder(d.client),
		newRoleBuilder(d.client),
	}
}

// Asset takes an input AssetRef and attempts to fetch it using the connector's
// authenticated http client. It streams a response, always starting with a
// metadata object, following by chunked payloads for the asset.
func (d *Salesforce) Asset(ctx context.Context, asset *v2.AssetRef) (string, io.ReadCloser, error) {
	return "", nil, nil
}

// Metadata returns metadata about the connector.
func (d *Salesforce) Metadata(ctx context.Context) (*v2.ConnectorMetadata, error) {
	return &v2.ConnectorMetadata{
		DisplayName: "Salesforce",
		Description: "Connector syncing Salesforce users",
	}, nil
}

// Validate is called to ensure that the connector is properly configured. It
// should exercise any API credentials to be sure that they are valid.
func (d *Salesforce) Validate(ctx context.Context) (annotations.Annotations, error) {
	_, ratelimitData, err := d.client.GetInfo(ctx)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	return outputAnnotations, err
}

// SetTokenSource this method makes Salesforce implement the OAuth2Connector
// interface. When an OAuth2Connector is created, this method gets called.
func (d *Salesforce) SetTokenSource(tokenSource oauth2.TokenSource) {
	logger := ctxzap.Extract(d.ctx)
	logger.Debug("baton-salesforce: SetTokenSource start")

	token, err := tokenSource.Token()
	if err != nil {
		panic(fmt.Sprintf("baton-salesforce: tokenSource could not get a token %s", err.Error()))
	}
	salesforceClient, err := client.NewSalesforceClient(
		d.ctx,
		d.instanceURL,
		token.AccessToken,
	)
	if err != nil {
		panic(fmt.Sprintf("baton-salesforce: could not create a client %s", err))
	}
	d.client = salesforceClient
}

// New returns a new instance of the connector.
func New(
	ctx context.Context,
	instanceURL string,
	useUsernameForEmail bool,
	accessToken string,
) (*Salesforce, error) {
	logger := ctxzap.Extract(ctx)
	instanceURL, err := fallBackToHTTPS(instanceURL)
	if err != nil {
		return nil, err
	}

	logger.Debug(
		"New Salesforce connector",
		zap.String("instanceURL", instanceURL),
		zap.String("accessToken", accessToken),
		zap.Bool("useUsernameForEmail", useUsernameForEmail),
	)

	// If no security token is passed in (i.e. is ""), then instantiate with a
	// broken  client. Client is later overwritten when .SetTokenSource() is called.
	salesforceClient, err := client.NewSalesforceClient(
		ctx,
		instanceURL,
		accessToken,
	)
	if err != nil {
		return nil, err
	}

	salesforce := Salesforce{
		client:                    salesforceClient,
		ctx:                       ctx,
		shouldUseUsernameForEmail: useUsernameForEmail,
		instanceURL:               instanceURL,
	}
	return &salesforce, nil
}
