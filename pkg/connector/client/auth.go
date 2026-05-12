package client

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"golang.org/x/oauth2/jwt"
)

const oauthTokenPath = "/services/oauth2/token" //nolint:gosec // false positive: this is an API path, not a credential

// NewJWTBearerTokenSource exchanges a signed JWT assertion for a Salesforce access token (RFC 7523).
func NewJWTBearerTokenSource(ctx context.Context, clientID, subject, loginURL string, privateKey []byte) (oauth2.TokenSource, error) {
	if loginURL == "" {
		return nil, fmt.Errorf("baton-salesforce: loginURL must not be empty")
	}
	base := strings.TrimRight(loginURL, "/")
	cfg := &jwt.Config{
		Email:      clientID, // maps to the JWT "iss" claim — Salesforce expects the Consumer Key here
		Subject:    subject,
		PrivateKey: privateKey,
		TokenURL:   base + oauthTokenPath,
		Audience:   base,
	}
	ts := cfg.TokenSource(ctx)
	// Validate credentials eagerly so errors surface at startup.
	tok, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("baton-salesforce: JWT bearer token exchange failed: %w", err)
	}
	return oauth2.ReuseTokenSource(tok, ts), nil
}

// NewClientCredentialsTokenSource obtains a Salesforce access token via the OAuth 2.0 client credentials flow.
func NewClientCredentialsTokenSource(ctx context.Context, clientID, clientSecret, instanceURL string) (oauth2.TokenSource, error) {
	cfg := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     strings.TrimRight(instanceURL, "/") + oauthTokenPath,
		AuthStyle:    oauth2.AuthStyleInParams, // Salesforce expects credentials in the request body, not Basic Auth
	}
	ts := cfg.TokenSource(ctx)
	// Validate credentials eagerly so errors surface at startup.
	tok, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("baton-salesforce: client credentials token exchange failed: %w", err)
	}
	return oauth2.ReuseTokenSource(tok, ts), nil
}
