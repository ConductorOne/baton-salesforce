//go:generate go run ./gen

package config

import (
	"github.com/conductorone/baton-sdk/pkg/field"
)

const (
	SalesforceUsernamePasswordGroup    = "username-password-group"
	SalesforceOAuthGroup               = "oauth-group"
	SalesforceJWTBearerGroup           = "jwt-bearer-group"
	SalesforceClientCredentialsGroup   = "client-credentials-group"
)

var (
	InstanceUrlField = field.StringField(
		"instance-url",
		field.WithDisplayName("Instance URL"),
		field.WithDescription("Your Salesforce domain, ex: acme.my.salesforce.com"),
		field.WithRequired(true),
	)
	UseUsernameForEmailField = field.BoolField(
		"user-username-for-email",
		field.WithDisplayName("Use Username for Email"),
		field.WithDescription("Use Salesforce usernames for email"),
	)
	UsernameField = field.StringField(
		"salesforce-username",
		field.WithDisplayName("Salesforce Username"),
		field.WithDescription("Salesforce account username"),
		field.WithRequired(true),
	)
	PasswordField = field.StringField(
		"salesforce-password",
		field.WithDisplayName("Salesforce Password"),
		field.WithDescription("Salesforce account password"),
		field.WithIsSecret(true),
		field.WithRequired(true),
	)
	Oauth2TokenField = field.Oauth2Field(
		"oauth2-token",
		field.WithDisplayName("OAuth Authentication"),
		field.WithDescription("The OAuth Authentication field"),
	)
	SecurityTokenField = field.StringField(
		"security-token",
		field.WithDisplayName("Security Token"),
		field.WithDescription("Salesforce security token (optional if trusted IP is configured)"),
		field.WithIsSecret(true),
	)
	SalesforceClientIdField = field.StringField(
		"salesforce-client-id",
		field.WithDisplayName("Salesforce Client ID"),
		field.WithDescription("OAuth Client ID (Consumer Key) from Connected App or External Client App"),
		field.WithRequired(true),
	)
	SalesforcePrivateKeyField = field.StringField(
		"salesforce-private-key",
		field.WithDisplayName("Salesforce Private Key"),
		field.WithDescription("PEM-encoded private key for JWT Bearer signing"),
		field.WithIsSecret(true),
		field.WithRequired(true),
	)
	SalesforceJwtSubjectField = field.StringField(
		"salesforce-jwt-subject",
		field.WithDisplayName("Salesforce JWT Subject"),
		field.WithDescription("Salesforce username to impersonate (sub claim in JWT)"),
		field.WithRequired(true),
	)
	SalesforceLoginUrlField = field.StringField(
		"salesforce-login-url",
		field.WithDisplayName("Salesforce Login URL"),
		field.WithDescription("Salesforce login URL for OAuth token endpoint (default: https://login.salesforce.com)"),
	)
	SalesforceOAuthClientSecretField = field.StringField(
		"salesforce-client-secret",
		field.WithDisplayName("Salesforce Client Secret"),
		field.WithDescription("OAuth Client Secret (Consumer Secret) from Connected App or External Client App"),
		field.WithIsSecret(true),
		field.WithRequired(true),
	)
	SyncConnectedApps = field.BoolField(
		"sync-connected-apps",
		field.WithDisplayName("Sync Connected Apps"),
		field.WithDescription("Optionally sync access to connected apps"),
	)
	SyncDeactivatedUsers = field.BoolField(
		"sync-deactivated-users",
		field.WithDisplayName("Sync Deactivated Users"),
		field.WithDescription("Optionally sync deactivated users"),
		field.WithDefaultValue(true),
	)
	SyncNonStandardUsers = field.BoolField(
		"sync-non-standard-users",
		field.WithDisplayName("Sync Non-Standard Users"),
		field.WithDescription("Optionally sync non-standard user types (Customer Community, etc)"),
		field.WithDefaultValue(false),
	)
	LicenseToLeastPrivilegedProfileMapping = field.StringMapField(
		"license-to-least-privileged-profile-mapping",
		field.WithDisplayName("License to Least Privileged Profile Mapping"),
		field.WithDescription("Mapping of Salesforce license types to least privileged profiles"),
	)

	configurationFields = []field.SchemaField{
		UsernameField,
		PasswordField,
		SecurityTokenField,
		InstanceUrlField,
		UseUsernameForEmailField,
		SyncConnectedApps,
		SyncDeactivatedUsers,
		SyncNonStandardUsers,
		LicenseToLeastPrivilegedProfileMapping,
		Oauth2TokenField,
		SalesforceClientIdField,
		SalesforcePrivateKeyField,
		SalesforceJwtSubjectField,
		SalesforceLoginUrlField,
		SalesforceOAuthClientSecretField,
	}

	Configuration = field.NewConfiguration(
		configurationFields,
		field.WithConnectorDisplayName("Salesforce"),
		field.WithHelpUrl("/docs/baton/salesforce"),
		field.WithIconUrl("/static/app-icons/salesforce.svg"),
		field.WithFieldGroups([]field.SchemaFieldGroup{
			{
				Name:        SalesforceUsernamePasswordGroup,
				DisplayName: "Username and password (legacy)",
				HelpText:    "Use a username and password for authentication. This method is deprecated and not supported by Salesforce External Client Apps.",
				Fields: []field.SchemaField{
					UsernameField,
					PasswordField,
					SecurityTokenField,
					InstanceUrlField,
					UseUsernameForEmailField,
					SyncConnectedApps,
					SyncDeactivatedUsers,
					SyncNonStandardUsers,
					LicenseToLeastPrivilegedProfileMapping,
				},
				Default: false,
			},
			{
				Name:        SalesforceOAuthGroup,
				DisplayName: "OAuth",
				HelpText:    "Use OAuth for authentication.",
				Fields: []field.SchemaField{
					InstanceUrlField,
					UseUsernameForEmailField,
					SyncConnectedApps,
					SyncDeactivatedUsers,
					SyncNonStandardUsers,
					LicenseToLeastPrivilegedProfileMapping,
					Oauth2TokenField,
				},
				Default: true,
			},
			{
				Name:        SalesforceJWTBearerGroup,
				DisplayName: "JWT Bearer (ECA compatible)",
				HelpText:    "Use JWT Bearer flow for server-to-server authentication. Compatible with both Connected Apps and External Client Apps (ECA). Requires a private key for JWT signing.",
				Fields: []field.SchemaField{
					SalesforceClientIdField,
					SalesforcePrivateKeyField,
					SalesforceJwtSubjectField,
					SalesforceLoginUrlField,
					InstanceUrlField,
					UseUsernameForEmailField,
					SyncConnectedApps,
					SyncDeactivatedUsers,
					SyncNonStandardUsers,
					LicenseToLeastPrivilegedProfileMapping,
				},
				Default: false,
			},
			{
				Name:        SalesforceClientCredentialsGroup,
				DisplayName: "Client Credentials (ECA compatible)",
				HelpText:    "Use Client Credentials flow for server-to-server authentication. Compatible with both Connected Apps and External Client Apps (ECA). Simpler than JWT Bearer but requires a client secret.",
				Fields: []field.SchemaField{
					SalesforceClientIdField,
					SalesforceOAuthClientSecretField,
					SalesforceLoginUrlField,
					InstanceUrlField,
					UseUsernameForEmailField,
					SyncConnectedApps,
					SyncDeactivatedUsers,
					SyncNonStandardUsers,
					LicenseToLeastPrivilegedProfileMapping,
				},
				Default: false,
			},
		}),
	)
)
