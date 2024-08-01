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

	configurationFields = []field.SchemaField{
		InstanceUrlField,
		UseUsernameForEmailField,
	}
	Configuration = field.NewConfiguration(configurationFields)
)
