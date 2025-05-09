package main

import (
	"context"
	"fmt"
	"os"

	"github.com/conductorone/baton-salesforce/pkg/connector"
	"github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/field"
	"github.com/conductorone/baton-sdk/pkg/types"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var version = "dev"

func main() {
	ctx := context.Background()

	_, cmd, err := config.DefineConfiguration(
		ctx,
		"baton-salesforce",
		getConnector,
		Configuration,
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

func getConnector(ctx context.Context, v *viper.Viper) (types.ConnectorServer, error) {
	l := ctxzap.Extract(ctx)
	err := field.Validate(Configuration, v)
	if err != nil {
		return nil, err
	}

	cb, err := connector.New(
		ctx,
		v.GetString(InstanceUrlField.FieldName),
		v.GetBool(UseUsernameForEmailField.FieldName),
		v.GetString(UsernameField.FieldName),
		v.GetString(PasswordField.FieldName),
		v.GetString(SecurityTokenField.FieldName),
		v.GetBool(SyncConnectedApps.FieldName),
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
