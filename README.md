![Baton Logo](./docs/images/baton-logo.png)

# `baton-salesforce` [![Go Reference](https://pkg.go.dev/badge/github.com/conductorone/baton-salesforce.svg)](https://pkg.go.dev/github.com/conductorone/baton-salesforce) ![ci](https://github.com/conductorone/baton-salesforce/actions/workflows/ci.yaml/badge.svg) ![verify](https://github.com/conductorone/baton-salesforce/actions/workflows/verify.yaml/badge.svg)

`baton-salesforce` is a connector for built using the [Baton SDK](https://github.com/conductorone/baton-sdk).

Check out [Baton](https://github.com/conductorone/baton) to learn more the project in general.

# Getting Started

## brew

```
brew install conductorone/baton/baton conductorone/baton/baton-salesforce
baton-salesforce
baton resources
```

## docker

```
docker run --rm -v $(pwd):/out -e BATON_INSTANCE_URL=acme.my.salesforce.com -e BATON_USER_USERNAME_FOR_EMAIL=false ghcr.io/conductorone/baton-salesforce:latest -f "/out/sync.c1z"
docker run --rm -v $(pwd):/out ghcr.io/conductorone/baton:latest -f "/out/sync.c1z" resources
```

## source

```
go install github.com/conductorone/baton/cmd/baton@main
go install github.com/conductorone/baton-salesforce/cmd/baton-salesforce@main

BATON_INSTANCE_URL=acme.my.salesforce.com BATON_USER_USERNAME_FOR_EMAIL=false baton-salesforce

baton resources
```

# Data Model

`baton-salesforce` will pull down information about the following resources:
- Users

# Contributing, Support and Issues

We started Baton because we were tired of taking screenshots and manually
building spreadsheets. We welcome contributions, and ideas, no matter how
small&mdash;our goal is to make identity and permissions sprawl less painful for
everyone. If you have questions, problems, or ideas: Please open a GitHub Issue!

See [CONTRIBUTING.md](https://github.com/ConductorOne/baton/blob/main/CONTRIBUTING.md) for more details.

# `baton-salesforce` Command Line Usage

```
baton-salesforce

Usage:
  baton-salesforce [flags]
  baton-salesforce [command]

Available Commands:
  capabilities       Get connector capabilities
  completion         Generate the autocompletion script for the specified shell
  config             Get the connector config schema
  help               Help about any command

Flags:
      --client-id string                                             The client ID used to authenticate with ConductorOne ($BATON_CLIENT_ID)
      --client-secret string                                         The client secret used to authenticate with ConductorOne ($BATON_CLIENT_SECRET)
      --external-resource-c1z string                                 The path to the c1z file to sync external baton resources with ($BATON_EXTERNAL_RESOURCE_C1Z)
      --external-resource-entitlement-id-filter string               The entitlement that external users, groups must have access to sync external baton resources ($BATON_EXTERNAL_RESOURCE_ENTITLEMENT_ID_FILTER)
  -f, --file string                                                  The path to the c1z file to sync with ($BATON_FILE) (default "sync.c1z")
  -h, --help                                                         help for baton-salesforce
      --instance-url string                                          required: Your Salesforce domain, ex: acme.my.salesforce.com ($BATON_INSTANCE_URL)
      --license-to-least-privileged-profile-mapping stringToString   Mapping of Salesforce license types to least privileged profiles ($BATON_LICENSE_TO_LEAST_PRIVILEGED_PROFILE_MAPPING) (default [])
      --log-format string                                            The output format for logs: json, console ($BATON_LOG_FORMAT) (default "console")
      --log-level string                                             The log level: debug, info, warn, error ($BATON_LOG_LEVEL) (default "info")
      --log-level-debug-expires-at string                            The timestamp indicating when debug-level logging should expire ($BATON_LOG_LEVEL_DEBUG_EXPIRES_AT)
      --otel-collector-endpoint string                               The endpoint of the OpenTelemetry collector to send observability data to (used for both tracing and logging if specific endpoints are not provided) ($BATON_OTEL_COLLECTOR_ENDPOINT)
  -p, --provisioning                                                 This must be set in order for provisioning actions to be enabled ($BATON_PROVISIONING)
      --salesforce-password string                                   Salesforce account password ($BATON_SALESFORCE_PASSWORD)
      --salesforce-username string                                   Salesforce account username ($BATON_SALESFORCE_USERNAME)
      --security-token string                                        Salesforce security token (optional if trusted IP is configured) ($BATON_SECURITY_TOKEN)
      --skip-entitlements-and-grants                                 This must be set to skip syncing of entitlements and grants ($BATON_SKIP_ENTITLEMENTS_AND_GRANTS)
      --skip-full-sync                                               This must be set to skip a full sync ($BATON_SKIP_FULL_SYNC)
      --sync-connected-apps                                          Optionally sync access to connected apps ($BATON_SYNC_CONNECTED_APPS)
      --sync-deactivated-users                                       Optionally sync deactivated users ($BATON_SYNC_DEACTIVATED_USERS) (default true)
      --sync-non-standard-users                                      Optionally sync non-standard user types (Customer Community, etc) ($BATON_SYNC_NON_STANDARD_USERS)
      --sync-resources strings                                       The resource IDs to sync ($BATON_SYNC_RESOURCES)
      --ticketing                                                    This must be set to enable ticketing support ($BATON_TICKETING)
      --user-username-for-email                                      Use Salesforce usernames for email ($BATON_USER_USERNAME_FOR_EMAIL)
  -v, --version                                                      version for baton-salesforce

Use "baton-salesforce [command] --help" for more information about a command.
```
