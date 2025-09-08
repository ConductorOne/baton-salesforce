package main

import (
	"github.com/conductorone/baton-sdk/pkg/config"
	cfg "github.com/conductorone/baton-twingate/pkg/config"
)

func main() {
	config.Generate("salesforce", cfg.Config)
}
