package config

import (
	"github.com/conductorone/baton-sdk/pkg/field"
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
	)
	PasswordField = field.StringField(
		"salesforce-password",
		field.WithDisplayName("Salesforce Password"),
		field.WithDescription("Salesforce account password"),
		field.WithIsSecret(true),
	)
	SecurityTokenField = field.StringField(
		"security-token",
		field.WithDisplayName("Security Token"),
		field.WithDescription("Salesforce security token (optional if trusted IP is configured)"),
		field.WithIsSecret(true),
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

	fieldRelationships = []field.SchemaFieldRelationship{
		field.FieldsRequiredTogether(
			UsernameField,
			PasswordField,
		),
	}

	configurationFields = []field.SchemaField{
		InstanceUrlField,
		UseUsernameForEmailField,
		UsernameField,
		PasswordField,
		SecurityTokenField,
		SyncConnectedApps,
		SyncDeactivatedUsers,
		LicenseToLeastPrivilegedProfileMapping,
		SyncNonStandardUsers,
	}

	Configuration = field.NewConfiguration(
		configurationFields,
		field.WithConstraints(fieldRelationships...),
		field.WithConnectorDisplayName("Salesforce"),
		field.WithHelpUrl("/docs/baton/salesforce"),
		field.WithIconUrl("/static/app-icons/salesforce.svg"),
	)
)
