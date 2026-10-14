package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNewClientCredentialsTokenSourceErrorCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       codes.Code
	}{
		{"invalid client", http.StatusBadRequest, `{"error":"invalid_client","error_description":"invalid client credentials"}`, codes.Unauthenticated},
		{"unauthorized", http.StatusUnauthorized, `{"error":"invalid_token"}`, codes.Unauthenticated},
		{"forbidden", http.StatusForbidden, `{"error":"insufficient_scope"}`, codes.PermissionDenied},
		{"server error", http.StatusInternalServerError, `{"error":"server_error"}`, codes.Unknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			_, err := NewClientCredentialsTokenSource(context.Background(), "id", "bad-secret", srv.URL)
			require.Error(t, err)
			require.Equal(t, tt.want, status.Code(err))
		})
	}
}
