package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestTokenExchangeErrorClassification pins how a failed token exchange is
// reported to exit.LogExit. A rejected credential must exit 16 (Unauthenticated)
// for the shared sync-test workflow's auth-error check; a transient failure must
// not, or an operator reads an org outage as bad credentials.
func TestTokenExchangeErrorClassification(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name         string
		status       int
		body         string
		expectedCode codes.Code
	}{
		{
			name:         "bad client secret",
			status:       http.StatusBadRequest,
			body:         `{"error":"invalid_client","error_description":"invalid client credentials"}`,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "unauthorized",
			status:       http.StatusUnauthorized,
			body:         `{"error":"invalid_client_id"}`,
			expectedCode: codes.Unauthenticated,
		},
		{
			// Credentials are fine, the org just won't grant the request. The
			// sync-test auth-error check accepts exit 7 as well as 16.
			name:         "forbidden is a refusal, not a bad credential",
			status:       http.StatusForbidden,
			body:         `{"error":"access_denied"}`,
			expectedCode: codes.PermissionDenied,
		},
		{
			// Salesforce serves HTTP 420 with an HTML interstitial while an org
			// is spinning up. The credentials may be perfectly good.
			name:         "org unavailable is not a credential problem",
			status:       420,
			body:         `<html><body>Stay Tuned... We are setting things up.</body></html>`,
			expectedCode: codes.Unknown,
		},
		{
			name:         "server error is not a credential problem",
			status:       http.StatusInternalServerError,
			body:         `{"error":"server_error"}`,
			expectedCode: codes.Unknown,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(testCase.status)
				_, _ = writer.Write([]byte(testCase.body))
			}))
			t.Cleanup(server.Close)

			_, err := NewClientCredentialsTokenSource(ctx, "client-id", "client-secret", server.URL)
			require.Error(t, err)
			require.Equal(t, testCase.expectedCode, status.Code(err))

			// Whichever way it is classified, the cause stays reachable —
			// uhttp.WrapErrors joins rather than formatting into a string.
			var retrieveErr *oauth2.RetrieveError
			require.True(t, errors.As(err, &retrieveErr), "lost the underlying *oauth2.RetrieveError")
			require.Equal(t, testCase.status, retrieveErr.Response.StatusCode)
		})
	}
}
