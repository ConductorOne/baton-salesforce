package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"golang.org/x/oauth2/jwt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const oauthTokenPath = "/services/oauth2/token" //nolint:gosec // false positive: this is an API path, not a credential

// NewJWTBearerTokenSource exchanges a signed JWT assertion for a Salesforce access token (RFC 7523).
func NewJWTBearerTokenSource(ctx context.Context, clientID, subject, loginURL string, privateKey []byte) (oauth2.TokenSource, error) {
	if loginURL == "" {
		return nil, fmt.Errorf("baton-salesforce: loginURL must not be empty")
	}
	u, err := url.Parse(loginURL)
	if err != nil {
		return nil, fmt.Errorf("baton-salesforce: invalid loginURL: %w", err)
	}
	cfg := &jwt.Config{
		Email:      clientID, // maps to the JWT "iss" claim — Salesforce expects the Consumer Key here
		Subject:    subject,
		PrivateKey: privateKey,
		TokenURL:   u.JoinPath(oauthTokenPath).String(),
		Audience:   u.String(),
	}
	ts := cfg.TokenSource(ctx)
	// Validate credentials eagerly so errors surface at startup.
	tok, err := ts.Token()
	if err != nil {
		return nil, tokenExchangeError("JWT bearer", err)
	}
	return oauth2.ReuseTokenSource(tok, ts), nil
}

// NewClientCredentialsTokenSource obtains a Salesforce access token via the OAuth 2.0 client credentials flow.
func NewClientCredentialsTokenSource(ctx context.Context, clientID, clientSecret, instanceURL string) (oauth2.TokenSource, error) {
	if instanceURL == "" {
		return nil, fmt.Errorf("baton-salesforce: instanceURL must not be empty")
	}
	u, err := url.Parse(instanceURL)
	if err != nil {
		return nil, fmt.Errorf("baton-salesforce: invalid instanceURL: %w", err)
	}
	cfg := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     u.JoinPath(oauthTokenPath).String(),
		AuthStyle:    oauth2.AuthStyleInParams, // Salesforce expects credentials in the request body, not Basic Auth
	}
	ts := cfg.TokenSource(ctx)
	// Validate credentials eagerly so errors surface at startup.
	tok, err := ts.Token()
	if err != nil {
		return nil, tokenExchangeError("client credentials", err)
	}
	return oauth2.ReuseTokenSource(tok, ts), nil
}

// tokenExchangeError maps a rejected token request to a gRPC status so the
// connector exits with a meaningful code instead of Unknown. Salesforce returns
// 400 for invalid_client/invalid_grant, so 400 maps to Unauthenticated rather
// than the uhttp default of InvalidArgument.
func tokenExchangeError(flow string, err error) error {
	msg := fmt.Sprintf("baton-salesforce: %s token exchange failed: %v", flow, err)
	var retrieveErr *oauth2.RetrieveError
	if errors.As(err, &retrieveErr) && retrieveErr.Response != nil {
		switch code := retrieveErr.Response.StatusCode; code {
		case http.StatusBadRequest, http.StatusUnauthorized:
			return status.Error(codes.Unauthenticated, msg)
		case http.StatusForbidden:
			return status.Error(codes.PermissionDenied, msg)
		default:
			return status.Error(uhttp.GrpcCodeFromHTTPStatus(code), msg)
		}
	}
	return fmt.Errorf("baton-salesforce: %s token exchange failed: %w", flow, err)
}
