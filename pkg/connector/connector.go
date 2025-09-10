package connector

import (
	"context"
	"fmt"
	"io"
	"net/url"

	"github.com/conductorone/baton-salesforce/pkg/config"
	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	configpb "github.com/conductorone/baton-sdk/pb/c1/config/v1"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/actions"
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
	client                       *client.SalesforceClient
	ctx                          context.Context
	instanceURL                  string
	shouldUseUsernameForEmail    bool
	syncConnectedApps            bool
	syncDeactivatedUsers         bool
	syncNonStandardUsers         bool
	licenseToLeastProfileMapping map[string]string
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
	rv := []connectorbuilder.ResourceSyncer{
		newUserBuilder(d.client, d.shouldUseUsernameForEmail, d.syncDeactivatedUsers, d.syncNonStandardUsers),
		newGroupBuilder(d.client),
		newPermissionBuilder(d.client),
		newProfileBuilder(d.client, d.licenseToLeastProfileMapping),
		newRoleBuilder(d.client),
		newPermissionSetGroupBuilder(d.client),
	}
	if d.syncConnectedApps {
		rv = append(rv, newConnectedApplicationBuilder(d.client))
	}
	return rv
}

// Asset takes an input AssetRef and attempts to fetch it using the connector's
// authenticated http client. It streams a response, always starting with a
// metadata object, following by chunked payloads for the asset.
func (d *Salesforce) Asset(ctx context.Context, asset *v2.AssetRef) (string, io.ReadCloser, error) {
	return "", nil, nil
}

// Metadata returns metadata about the connector.
func (d *Salesforce) Metadata(ctx context.Context) (*v2.ConnectorMetadata, error) {
	defaultTimeZone := "America/New_York"

	return &v2.ConnectorMetadata{
		DisplayName: "Salesforce",
		Description: "Connector syncing Salesforce users",
		AccountCreationSchema: &v2.ConnectorAccountCreationSchema{
			FieldMap: map[string]*v2.ConnectorAccountCreationSchema_Field{
				"email": {
					DisplayName: "Email",
					Required:    true,
					Description: "This email will be used as the login for the user.",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "Email",
					Order:       1,
				},
				"profileId": {
					DisplayName: "Profile ID",
					Required:    true,
					Description: "Salesforce Profile ID",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "ProfileId",
					Order:       2,
				},
				"alias": {
					DisplayName: "Alias",
					Required:    true,
					Description: "User Alias",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "Alias",
					Order:       3,
				},
				"first_name": {
					DisplayName: "First Name",
					Required:    true,
					Description: "User first name",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "FirstName",
					Order:       5,
				},
				"last_name": {
					DisplayName: "Last Name",
					Required:    true,
					Description: "User last name",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "LastName",
					Order:       4,
				},
				"timezone": {
					DisplayName: "Time Zone",
					Required:    true,
					Description: "User time zone",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{
							DefaultValue: &defaultTimeZone,
						},
					},
					Placeholder: "TimeZone",
					Order:       6,
				},
			},
		},
	}, nil
}

var updateUserStatusActionSchema = &v2.BatonActionSchema{
	Name: "update_user_status",
	Arguments: []*configpb.Field{
		{
			Name:        "resource_id",
			DisplayName: "User Resource ID",
			Description: "The ID of the user resource to update the status of",
			Field:       &configpb.Field_StringField{},
			IsRequired:  true,
		},
		{
			Name:        "is_active",
			DisplayName: "Is Active",
			Description: "Update the user status to active or inactive",
			Field:       &configpb.Field_BoolField{},
			IsRequired:  true,
		},
	},
	ReturnTypes: []*configpb.Field{
		{
			Name:        "success",
			DisplayName: "Success",
			Description: "Whether the user resource status was updated successfully",
			Field:       &configpb.Field_BoolField{},
		},
	},
}

func (d *Salesforce) RegisterActionManager(ctx context.Context) (connectorbuilder.CustomActionManager, error) {
	l := ctxzap.Extract(ctx)

	actionManager := actions.NewActionManager(ctx)
	err := actionManager.RegisterAction(ctx, "update_user_status", updateUserStatusActionSchema, d.updateUserStatus)
	if err != nil {
		l.Error("failed to register action", zap.Error(err))
		return nil, err
	}

	return actionManager, nil
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
	d.client.TokenSource = tokenSource
}

// New returns a new instance of the connector using the provided configuration.
func New(ctx context.Context, cfg *config.Salesforce) (*Salesforce, error) {
	logger := ctxzap.Extract(ctx)
	instanceURL, err := fallBackToHTTPS(cfg.InstanceUrl)
	if err != nil {
		return nil, err
	}

	logger.Debug(
		"New Salesforce connector",
		zap.String("instanceURL", instanceURL),
		zap.String("username", cfg.SalesforceUsername),
		zap.Bool("password?", cfg.SalesforcePassword != ""),
		zap.Bool("securityToken?", cfg.SecurityToken != ""),
		zap.Bool("useUsernameForEmail", cfg.UserUsernameForEmail),
		zap.Bool("syncConnectedApps", cfg.SyncConnectedApps),
		zap.Bool("syncDeactivatedUsers", cfg.SyncDeactivatedUsers),
		zap.Bool("syncNonStandardUsers", cfg.SyncNonStandardUsers),
		zap.Any("licenseToLeastProfileMapping", cfg.GetLicenseToLeastPrivilegedProfileMapping()),
	)

	// Instantiate with a "broken" client. Client is later overwritten either
	// when .SetTokenSource() or .LoginPassword() are called.
	var tokenSource oauth2.TokenSource
	salesforceClient := client.New(
		instanceURL,
		tokenSource,
		cfg.SalesforceUsername,
		cfg.SalesforcePassword,
		cfg.SecurityToken,
	)
	salesforce := Salesforce{
		client:                       salesforceClient,
		ctx:                          ctx,
		shouldUseUsernameForEmail:    cfg.UserUsernameForEmail,
		instanceURL:                  instanceURL,
		syncConnectedApps:            cfg.SyncConnectedApps,
		syncDeactivatedUsers:         cfg.SyncDeactivatedUsers,
		syncNonStandardUsers:         cfg.SyncNonStandardUsers,
		licenseToLeastProfileMapping: cfg.GetLicenseToLeastPrivilegedProfileMapping(),
	}
	return &salesforce, nil
}
