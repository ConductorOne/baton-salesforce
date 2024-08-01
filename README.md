![Baton Logo](./docs/images/baton-logo.png)

# `baton-salesforce` [![Go Reference](https://pkg.go.dev/badge/github.com/conductorone/baton-salesforce.svg)](https://pkg.go.dev/github.com/conductorone/baton-salesforce) ![main ci](https://github.com/conductorone/baton-salesforce/actions/workflows/main.yaml/badge.svg)

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
  help               Help about any command

Flags:
      --client-id string          The client ID used to authenticate with ConductorOne ($BATON_CLIENT_ID)
      --client-secret string      The client secret used to authenticate with ConductorOne ($BATON_CLIENT_SECRET)
  -f, --file string               The path to the c1z file to sync with ($BATON_FILE) (default "sync.c1z")
  -h, --help                      help for baton-salesforce
      --instance-url string       required: Your Salesforce domain, ex: acme.my.salesforce.com ($BATON_INSTANCE_URL)
      --log-format string         The output format for logs: json, console ($BATON_LOG_FORMAT) (default "json")
      --log-level string          The log level: debug, info, warn, error ($BATON_LOG_LEVEL) (default "info")
  -p, --provisioning              This must be set in order for provisioning actions to be enabled ($BATON_PROVISIONING)
      --ticketing                 This must be set to enable ticketing support ($BATON_TICKETING)
      --user-username-for-email   Use Salesforce usernames for email ($BATON_USER_USERNAME_FOR_EMAIL)
  -v, --version                   version for baton-salesforce

Use "baton-salesforce [command] --help" for more information about a command.
```
