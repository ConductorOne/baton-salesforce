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
	TODOTokenField = field.StringField(
		"todo-token",
		field.WithDescription("TODO MARCOS FIRST OAUTH"),
		field.WithRequired(true),
	)
	configurationFields = []field.SchemaField{
		InstanceUrlField,
		UseUsernameForEmailField,
		TODOTokenField,
	}
	configuration = field.NewConfiguration(configurationFields)
)
