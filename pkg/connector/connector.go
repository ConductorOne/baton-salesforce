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
	if err != nil {
		return outputAnnotations, err
	}

	// Some users are using credentials that lack access to the "PermissionSets"
	// table. Checking these credentials early.
	_, _, ratelimitData, err = d.client.GetPermissionSets(ctx, "", 1)
	outputAnnotations = client.WithRateLimitAnnotations(ratelimitData)
	return outputAnnotations, err
}

// SetTokenSource this method makes Salesforce implement the OAuth2Connector
// interface. When an OAuth2Connector is created, this method gets called.
func (d *Salesforce) SetTokenSource(tokenSource oauth2.TokenSource) {
	logger := ctxzap.Extract(d.ctx)
	logger.Debug("baton-salesforce: SetTokenSource start")
	d.client.TokenSource = tokenSource
}

// New returns a new instance of the connector.
func New(
	ctx context.Context,
	instanceURL string,
	useUsernameForEmail bool,
	username string,
	password string,
	securityToken string,
) (*Salesforce, error) {
	logger := ctxzap.Extract(ctx)
	instanceURL, err := fallBackToHTTPS(instanceURL)
	if err != nil {
		return nil, err
	}

	logger.Debug(
		"New Salesforce connector",
		zap.String("instanceURL", instanceURL),
		zap.String("username", username),
		zap.Bool("password?", password != ""),
		zap.Bool("securityToken?", securityToken != ""),
		zap.Bool("useUsernameForEmail", useUsernameForEmail),
	)

	// Instantiate with a "broken" client. Client is later overwritten either
	// when .SetTokenSource() or .LoginPassword() are called.
	var tokenSource oauth2.TokenSource
	salesforceClient := client.New(
		instanceURL,
		tokenSource,
		username,
		password,
		securityToken,
	)
	salesforce := Salesforce{
		client:                    salesforceClient,
		ctx:                       ctx,
		shouldUseUsernameForEmail: useUsernameForEmail,
		instanceURL:               instanceURL,
	}
	return &salesforce, nil
}
