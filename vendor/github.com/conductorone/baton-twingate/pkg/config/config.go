package config

import (
	"github.com/conductorone/baton-sdk/pkg/field"
)

var (
	DomainField = field.StringField(
		"domain",
		field.WithDisplayName("Domain"),
		field.WithDescription("The domain for your Salesforce account. ($BATON_DOMAIN)"),
		field.WithRequired(true),
	)
	ApiKeyField = field.StringField(
		"api-key",
		field.WithDisplayName("API Key"),
		field.WithDescription("The api key for your Salesforce account. ($BATON_API_KEY)"),
		field.WithRequired(true),
		field.WithIsSecret(true),
	)

	ConfigurationFields = []field.SchemaField{
		DomainField,
		ApiKeyField,
	}

	Configuration      = field.NewConfiguration(ConfigurationFields)
	FieldRelationships = []field.SchemaFieldRelationship{}
)

//go:generate go run ./gen
var Config = field.NewConfiguration(
	ConfigurationFields,
	field.WithConstraints(FieldRelationships...),
	field.WithConnectorDisplayName("Salesforce"),
	field.WithHelpUrl("/docs/baton/salesforce"),
	field.WithIconUrl("/static/app-icons/salesforce.svg"),
)
