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
	AccessTokenField = field.StringField(
		"access-token",
		field.WithDescription("Access token used to connect to the Salesforce REST API"),
	)

	configurationFields = []field.SchemaField{
		InstanceUrlField,
		UseUsernameForEmailField,
		AccessTokenField,
	}
	Configuration = field.NewConfiguration(configurationFields)
)
