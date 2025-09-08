package config

import (
	"github.com/conductorone/baton-sdk/pkg/field"
)

var (
	InstanceUrlField = field.StringField(
		"instance-url",
		field.WithDescription("Your Salesforce domain, ex: acme.my.salesforce.com"),
		field.WithRequired(true),
	)
	UseUsernameForEmailField = field.BoolField(
		"user-username-for-email",
		field.WithDescription("Use Salesforce usernames for email"),
	)
	UsernameField = field.StringField(
		"salesforce-username",
		field.WithDescription("Salesforce account username"),
	)
	PasswordField = field.StringField(
		"salesforce-password",
		field.WithDescription("Salesforce account password"),
		field.WithIsSecret(true),
	)
	SecurityTokenField = field.StringField(
		"security-token",
		field.WithDescription("Salesforce security token (optional if trusted IP is configured)"),
	)
	SyncConnectedApps = field.BoolField(
		"sync-connected-apps",
		field.WithDescription("Optionally sync access to connected apps"),
	)
	SyncDeactivatedUsers = field.BoolField(
		"sync-deactivated-users",
		field.WithDescription("Optionally sync deactivated users"),
		field.WithDefaultValue(true),
	)
	SyncNonStandardUsers = field.BoolField(
		"sync-non-standard-users",
		field.WithDescription("Sync non-standard Salesforce user types (e.g., Partner, Portal, Chatter). Defaults to false."),
	)
	LicenseToLeastPrivilegedProfileMapping = field.StringMapField(
		"license-to-least-privileged-profile-mapping",
		field.WithDescription("Mapping of Salesforce license types to least privileged profiles"),
	)

	configurationFields = []field.SchemaField{
		InstanceUrlField,
		UseUsernameForEmailField,
		UsernameField,
		PasswordField,
		SecurityTokenField,
		SyncConnectedApps,
		SyncDeactivatedUsers,
		SyncNonStandardUsers,
		LicenseToLeastPrivilegedProfileMapping,
	}

	Configuration = field.NewConfiguration(
		configurationFields,
		field.WithConnectorDisplayName("Twingate"),
		field.WithHelpUrl("/docs/baton/twingate"),
		field.WithIconUrl("/static/app-icons/twingate.svg"),
	)
)
