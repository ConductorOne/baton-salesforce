package main

import (
	"context"

	cfg "github.com/conductorone/baton-salesforce/pkg/config"
	"github.com/conductorone/baton-salesforce/pkg/connector"
	"github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorrunner"
)

var version = "dev"

func main() {
	ctx := context.Background()
	config.RunConnector(ctx,
		"baton-salesforce",
		version,
		cfg.Configuration,
		connector.New,
		connectorrunner.WithDefaultCapabilitiesConnectorBuilderV2(&connector.Salesforce{}),
	)
}
