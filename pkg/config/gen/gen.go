package main

import (
	cfg "github.com/conductorone/baton-salesforce/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/config"
)

func main() {
	config.Generate("salesforce", cfg.Configuration)
}
