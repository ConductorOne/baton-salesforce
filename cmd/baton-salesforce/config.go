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
	UsernameField = field.StringField(
		"salesforce-username",
		field.WithDescription("Salesforce account username"),
	)
	PasswordField = field.StringField(
		"salesforce-password",
		field.WithDescription("Salesforce account password"),
	)

	configurationFields = []field.SchemaField{
		InstanceUrlField,
		UseUsernameForEmailField,
		UsernameField,
		PasswordField,
	}

	Configuration = field.NewConfiguration(
		configurationFields,
		field.FieldsRequiredTogether(UsernameField, PasswordField),
	)
)
