package client

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const defaultLoginURL = "https://login.salesforce.com"

type JWTBearerTokenSource struct {
	clientID   string
	privateKey *rsa.PrivateKey
	subject    string
	loginURL   string
}

func NewJWTBearerTokenSource(clientID, privateKeyPEM, subject, loginURL string) (*JWTBearerTokenSource, error) {
	if loginURL == "" {
		loginURL = defaultLoginURL
	}

	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("baton-salesforce: failed to decode PEM block from private key")
	}

	var rsaKey *rsa.PrivateKey
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		rsaKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("baton-salesforce: failed to parse private key (tried PKCS8 and PKCS1): %w", err)
		}
	} else {
		var ok bool
		rsaKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("baton-salesforce: private key is not RSA")
		}
	}

	return &JWTBearerTokenSource{
		clientID:   clientID,
		privateKey: rsaKey,
		subject:    subject,
		loginURL:   loginURL,
	}, nil
}

func (s *JWTBearerTokenSource) Token() (*oauth2.Token, error) {
	now := time.Now()

	headerJSON, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return nil, fmt.Errorf("baton-salesforce: failed to marshal JWT header: %w", err)
	}

	claimsJSON, err := json.Marshal(map[string]interface{}{
		"iss": s.clientID,
		"sub": s.subject,
		"aud": s.loginURL,
		"exp": now.Add(5 * time.Minute).Unix(),
	})
	if err != nil {
		return nil, fmt.Errorf("baton-salesforce: failed to marshal JWT claims: %w", err)
	}

	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)

	hash := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return nil, fmt.Errorf("baton-salesforce: failed to sign JWT: %w", err)
	}

	assertion := signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)

	token, err := requestToken(s.loginURL, url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	})
	if err != nil {
		return nil, fmt.Errorf("baton-salesforce: JWT bearer token exchange failed: %w", err)
	}
	token.Expiry = now.Add(5 * time.Minute)
	return token, nil
}

type ClientCredentialsTokenSource struct {
	clientID     string
	clientSecret string
	loginURL     string
}

func NewClientCredentialsTokenSource(clientID, clientSecret, loginURL string) *ClientCredentialsTokenSource {
	if loginURL == "" {
		loginURL = defaultLoginURL
	}
	return &ClientCredentialsTokenSource{
		clientID:     clientID,
		clientSecret: clientSecret,
		loginURL:     loginURL,
	}
}

func (s *ClientCredentialsTokenSource) Token() (*oauth2.Token, error) {
	return requestToken(s.loginURL, url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {s.clientID},
		"client_secret": {s.clientSecret},
	})
}

func requestToken(loginURL string, data url.Values) (*oauth2.Token, error) {
	tokenURL := strings.TrimRight(loginURL, "/") + "/services/oauth2/token"

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("token request creation failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}

	return &oauth2.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
	}, nil
}
