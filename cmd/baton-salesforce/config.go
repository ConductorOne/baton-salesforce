package main

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
	SecurityTokenField = field.StringField(
		"security-token",
		field.WithDescription("Case-sensitive alphanumeric code that’s tied to your password"),
	)

	configurationFields = []field.SchemaField{
		InstanceUrlField,
		UseUsernameForEmailField,
		SecurityTokenField,
	}
	Configuration = field.NewConfiguration(configurationFields)
)
