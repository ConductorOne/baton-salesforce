package connector

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/conductorone/baton-salesforce/test"
	"github.com/conductorone/simpleforce"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/stretchr/testify/require"
)

// TestTerritoryInvalidTypeErrorMessage verifies that when Salesforce returns
// INVALID_TYPE for Territory2 (Enterprise Territory Management 2.0 not enabled),
// the sync still FAILS but the error names the prerequisite and the unblock,
// while remaining classified as a simpleforce.SalesforceError through the wrap.
func TestTerritoryInvalidTypeErrorMessage(t *testing.T) {
	ctx := context.Background()

	const invalidTypeBody = `[{"message":"sObject type 'Territory2' is not supported.","errorCode":"INVALID_TYPE"}]`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(invalidTypeBody))
	}))
	defer server.Close()

	salesforceClient, err := test.Client(ctx, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	c := newTerritoryBuilder(salesforceClient)

	resources, results, err := c.List(ctx, nil, rs.SyncOpAttrs{PageToken: pagination.Token{Size: 100}})
	require.Error(t, err)
	require.Nil(t, resources)
	require.NotNil(t, results)

	errMsg := err.Error()
	require.Contains(t, errMsg, "Enterprise Territory Management 2.0")
	require.Contains(t, errMsg, "INVALID_TYPE")
	require.Contains(t, errMsg, "baton-salesforce:")
	require.Contains(t, errMsg, "disable the Territory resource type in the connector config")

	var sfErr simpleforce.SalesforceError
	require.True(t, errors.As(err, &sfErr))
	require.Equal(t, "INVALID_TYPE", sfErr.ErrorCode)
	require.Contains(t, sfErr.Message, "sObject type 'Territory2' is not supported.")
}

// TestTerritoryNonInvalidTypePassthrough verifies that non-INVALID_TYPE errors
// from GetTerritories are returned value-identically: no ETM 2.0 prerequisite
// text is added, and the sync still fails.
func TestTerritoryNonInvalidTypePassthrough(t *testing.T) {
	ctx := context.Background()

	const duplicateValueBody = `[{"message":"duplicate value","errorCode":"DUPLICATE_VALUE"}]`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(duplicateValueBody))
	}))
	defer server.Close()

	salesforceClient, err := test.Client(ctx, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	c := newTerritoryBuilder(salesforceClient)

	resources, results, err := c.List(ctx, nil, rs.SyncOpAttrs{PageToken: pagination.Token{Size: 100}})
	require.Error(t, err)
	require.Nil(t, resources)
	require.NotNil(t, results)

	errMsg := err.Error()
	require.NotContains(t, errMsg, "Enterprise Territory Management 2.0")
	require.NotContains(t, errMsg, "disable the Territory resource type in the connector config")
}