package main

import (
	"context"
	"fmt"
	"os"

	config "github.com/conductorone/baton-salesforce/pkg/config"
	"github.com/conductorone/baton-salesforce/pkg/connector"
	sdkconfig "github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/field"
	"github.com/conductorone/baton-sdk/pkg/types"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

var version = "dev"

func main() {
	ctx := context.Background()

	_, cmd, err := sdkconfig.DefineConfiguration(
		ctx,
		"baton-salesforce",
		getConnector,
		config.Configuration,
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	cmd.Version = version

	err = cmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func getConnector(ctx context.Context, cfg *config.Salesforce) (types.ConnectorServer, error) {
	l := ctxzap.Extract(ctx)
	err := field.Validate(config.Configuration, cfg)
	if err != nil {
		return nil, err
	}

	cb, err := connector.New(
		ctx,
		cfg.GetString(config.InstanceUrlField.FieldName),
		cfg.GetBool(config.UseUsernameForEmailField.FieldName),
		cfg.GetString(config.UsernameField.FieldName),
		cfg.GetString(config.PasswordField.FieldName),
		cfg.GetString(config.SecurityTokenField.FieldName),
		cfg.GetBool(config.SyncConnectedApps.FieldName),
		cfg.GetBool(config.SyncDeactivatedUsers.FieldName),
		cfg.GetStringMapString(config.LicenseToLeastPrivilegedProfileMapping.FieldName),
	)
	if err != nil {
		l.Error("error creating connector", zap.Error(err))
		return nil, err
	}
	connector, err := connectorbuilder.NewConnector(ctx, cb)
	if err != nil {
		l.Error("error creating connector", zap.Error(err))
		return nil, err
	}
	return connector, nil
}
