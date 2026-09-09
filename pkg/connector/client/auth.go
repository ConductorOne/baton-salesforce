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
)

const oauthTokenPath = "/services/oauth2/token" //nolint:gosec // false positive: this is an API path, not a credential

// tokenExchangeError classifies a failed token exchange for exit.LogExit.
//
// A rejected credential is Unauthenticated (exit 16) and a refused one is
// PermissionDenied (exit 7); the shared sync-test workflow's auth-error check
// accepts either. Salesforce answers a bad client secret or an unsigned JWT with
// 400/401, and a 403 for a request whose credentials are fine but which the org
// will not grant — an app not approved for this user, say — so folding 403 in
// with the rest would report that to an operator as bad credentials.
//
// Everything else — a DNS failure, a timeout, a 5xx, or the HTTP 420
// interstitial Salesforce serves while an org is spinning up — is left
// unclassified rather than reported as an auth problem at all.
//
// uhttp.WrapErrors joins the status with the cause instead of formatting it into
// a string, so status.Code still resolves through the join while errors.Is /
// errors.As can still reach the underlying *oauth2.RetrieveError.
func tokenExchangeError(message string, err error) error {
	var retrieveErr *oauth2.RetrieveError
	if errors.As(err, &retrieveErr) && retrieveErr.Response != nil {
		switch retrieveErr.Response.StatusCode {
		case http.StatusBadRequest, http.StatusUnauthorized:
			return uhttp.WrapErrors(codes.Unauthenticated, message, err)
		case http.StatusForbidden:
			return uhttp.WrapErrors(codes.PermissionDenied, message, err)
		}
	}
	return fmt.Errorf("%s: %w", message, err)
}

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
		return nil, tokenExchangeError("baton-salesforce: JWT bearer token exchange failed", err)
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
		return nil, tokenExchangeError("baton-salesforce: client credentials token exchange failed", err)
	}
	return oauth2.ReuseTokenSource(tok, ts), nil
}
