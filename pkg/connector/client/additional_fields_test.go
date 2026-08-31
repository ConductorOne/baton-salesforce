package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// userQueryServer is a Salesforce stand-in that records the SOQL it is handed
// and lets a test control the describe and query responses.
type userQueryServer struct {
	mutex sync.Mutex
	// queries holds every SOQL string the client sent, in order.
	queries []string

	describe    func() (int, string)
	queryResult func(soql string) (int, string)
}

func (s *userQueryServer) recordedQueries() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return append([]string(nil), s.queries...)
}

func (s *userQueryServer) start(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set(uhttp.ContentType, "application/json")

		if strings.HasSuffix(request.URL.Path, "/describe") {
			status, body := s.describe()
			writer.WriteHeader(status)
			_, _ = writer.Write([]byte(body))
			return
		}

		soql := request.URL.Query().Get("q")
		s.mutex.Lock()
		s.queries = append(s.queries, soql)
		s.mutex.Unlock()

		status, body := s.queryResult(soql)
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// newTestClient builds a client pointed at a stub server, bypassing the shared
// test helper so these tests can also exercise the describe endpoint.
func newTestClient(t *testing.T, ctx context.Context, baseURL string, additionalFields []string) *SalesforceClient {
	t.Helper()

	// The uhttp client caches GET responses; a stale entry from an earlier test
	// would mask the request this one is asserting on.
	require.NoError(t, uhttp.ClearCaches(ctx))

	salesforceClient := New(
		baseURL,
		oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "mock-access-token"}),
		"",
		"",
		"",
	)
	require.NoError(t, salesforceClient.Initialize(ctx))
	salesforceClient.SetAdditionalUserFields(ctx, additionalFields)
	return salesforceClient
}

func usersResponse(t *testing.T, records ...map[string]any) string {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"totalSize": len(records),
		"done":      true,
		"records":   records,
	})
	require.NoError(t, err)
	return string(body)
}

func describeResponse(t *testing.T, fieldNames ...string) string {
	t.Helper()

	fields := make([]map[string]string, 0, len(fieldNames))
	for _, name := range fieldNames {
		fields = append(fields, map[string]string{"name": name})
	}
	body, err := json.Marshal(map[string]any{"fields": fields})
	require.NoError(t, err)
	return string(body)
}

func standardUserRecord(extra map[string]any) map[string]any {
	record := map[string]any{
		"Id":       "0051X",
		"Username": "user@example.com",
		"Email":    "user@example.com",
		"UserType": "Standard",
		"IsActive": true,
	}
	for key, value := range extra {
		record[key] = value
	}
	return record
}

func TestNormalizeAdditionalFields(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name       string
		configured []string
		expected   []string
	}{
		{
			name:       "custom picklist field",
			configured: []string{"Role_Based_Access__c"},
			expected:   []string{"Role_Based_Access__c"},
		},
		{
			name:       "trims whitespace and drops blanks",
			configured: []string{"  Role_Based_Access__c  ", "", "   "},
			expected:   []string{"Role_Based_Access__c"},
		},
		{
			name:       "namespaced custom field",
			configured: []string{"acme__Role_Based_Access__c"},
			expected:   []string{"acme__Role_Based_Access__c"},
		},
		{
			// Field names go into the SELECT clause verbatim — SOQL has no bind
			// parameters for them — so anything but a bare API name is dropped.
			name: "rejects SOQL injection attempts",
			configured: []string{
				"Id FROM User WHERE Id != null--",
				"(SELECT Id FROM Groups)",
				"Name, Email",
				"Role Based Access",
				"Role_Based_Access__c;DROP",
				"*",
				"1Field__c",
			},
			expected: []string{},
		},
		{
			// Relationship traversals can't be validated against one object's
			// describe, and are out of scope for this option.
			name:       "rejects relationship traversals",
			configured: []string{"Manager.Name"},
			expected:   []string{},
		},
		{
			name:       "drops fields already selected, case-insensitively",
			configured: []string{"Email", "userType", "Id", "Role_Based_Access__c"},
			expected:   []string{"Role_Based_Access__c"},
		},
		{
			name:       "drops duplicates, case-insensitively",
			configured: []string{"Role_Based_Access__c", "role_based_access__c", "Department"},
			expected:   []string{"Role_Based_Access__c", "Department"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(
				t,
				testCase.expected,
				NormalizeAdditionalFields(ctx, TableNameUsers, testCase.configured),
			)
		})
	}
}

func TestNormalizeAdditionalFieldsCapsCount(t *testing.T) {
	ctx := context.Background()

	configured := make([]string, 0, maxAdditionalFields+10)
	for i := range maxAdditionalFields + 10 {
		configured = append(configured, "Custom_"+strings.Repeat("x", i%5)+string(rune('a'+i%26))+"__c")
	}

	normalized := NormalizeAdditionalFields(ctx, TableNameUsers, configured)
	require.LessOrEqual(t, len(normalized), maxAdditionalFields)
}

// TestGetUsersWithAdditionalFields is the ticket's core case: a configured
// custom picklist is selected and its value reaches the user.
func TestGetUsersWithAdditionalFields(t *testing.T) {
	ctx := context.Background()

	stub := &userQueryServer{
		describe: func() (int, string) {
			return http.StatusOK, describeResponse(t, "Id", "Username", "Role_Based_Access__c")
		},
		queryResult: func(string) (int, string) {
			return http.StatusOK, usersResponse(t, standardUserRecord(map[string]any{
				"Role_Based_Access__c": "Tier 2 Support",
			}))
		},
	}
	server := stub.start(t)

	salesforceClient := newTestClient(t, ctx, server.URL, []string{"Role_Based_Access__c"})
	users, _, _, err := salesforceClient.GetUsers(ctx, "", 100, true, false)
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(
		t,
		map[string]any{"Role_Based_Access__c": "Tier 2 Support"},
		users[0].AdditionalFields,
	)

	queries := stub.recordedQueries()
	require.Len(t, queries, 1)
	require.Contains(t, queries[0], "Role_Based_Access__c")
	// The standard fields must still be selected alongside the custom one.
	require.Contains(t, queries[0], "Username")
	require.Contains(t, queries[0], "UserType = 'Standard'")
}

// TestGetUsersDropsUnknownAdditionalFields covers the misspelled-field case: the
// describe check keeps a bad name out of the SELECT, so the sync survives.
func TestGetUsersDropsUnknownAdditionalFields(t *testing.T) {
	ctx := context.Background()

	stub := &userQueryServer{
		describe: func() (int, string) {
			// Salesforce reports the canonical casing, which is what we select.
			return http.StatusOK, describeResponse(t, "Id", "Role_Based_Access__c")
		},
		queryResult: func(string) (int, string) {
			return http.StatusOK, usersResponse(t, standardUserRecord(map[string]any{
				"Role_Based_Access__c": "Tier 2 Support",
			}))
		},
	}
	server := stub.start(t)

	salesforceClient := newTestClient(t, ctx, server.URL, []string{
		"role_based_access__c", // right field, wrong case
		"Rolebased_Access__c",  // typo: does not exist on the object
	})
	users, _, _, err := salesforceClient.GetUsers(ctx, "", 100, true, false)
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(
		t,
		map[string]any{"Role_Based_Access__c": "Tier 2 Support"},
		users[0].AdditionalFields,
	)

	queries := stub.recordedQueries()
	require.Len(t, queries, 1)
	require.Contains(t, queries[0], "Role_Based_Access__c")
	require.NotContains(t, queries[0], "Rolebased_Access__c")
}

// TestGetUsersInvalidFieldFallback is the last line of defense: if a field slips
// past the describe check and Salesforce rejects the query, users still sync.
func TestGetUsersInvalidFieldFallback(t *testing.T) {
	ctx := context.Background()

	const invalidField = `[{"message":"\nSELECT Role_Based_Access__c\n       ^\nERROR at Row:1:Column:8\nNo such column 'Role_Based_Access__c' on entity 'User'.","errorCode":"INVALID_FIELD"}]`

	stub := &userQueryServer{
		describe: func() (int, string) {
			// Describe is unavailable, so the configured names are used as-is.
			return http.StatusInternalServerError, `[{"message":"boom","errorCode":"SERVER_ERROR"}]`
		},
		queryResult: func(soql string) (int, string) {
			if strings.Contains(soql, "Role_Based_Access__c") {
				return http.StatusBadRequest, invalidField
			}
			return http.StatusOK, usersResponse(t, standardUserRecord(nil))
		},
	}
	server := stub.start(t)

	salesforceClient := newTestClient(t, ctx, server.URL, []string{"Role_Based_Access__c"})
	users, _, _, err := salesforceClient.GetUsers(ctx, "", 100, true, false)
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Nil(t, users[0].AdditionalFields)

	queries := stub.recordedQueries()
	require.Len(t, queries, 2)
	require.Contains(t, queries[0], "Role_Based_Access__c")
	require.NotContains(t, queries[1], "Role_Based_Access__c")

	// The field stays off for the rest of the client's life, so page two doesn't
	// repeat the failure.
	require.NoError(t, uhttp.ClearCaches(ctx))
	_, _, _, err = salesforceClient.GetUsers(ctx, "", 100, true, false)
	require.NoError(t, err)
	queries = stub.recordedQueries()
	require.Len(t, queries, 3)
	require.NotContains(t, queries[2], "Role_Based_Access__c")
}

// TestGetUsersWithoutAdditionalFields pins the default: no config, no describe
// call, and the query is unchanged.
func TestGetUsersWithoutAdditionalFields(t *testing.T) {
	ctx := context.Background()

	stub := &userQueryServer{
		describe: func() (int, string) {
			t.Error("describe should not be called when no additional fields are configured")
			return http.StatusOK, describeResponse(t)
		},
		queryResult: func(string) (int, string) {
			return http.StatusOK, usersResponse(t, standardUserRecord(nil))
		},
	}
	server := stub.start(t)

	salesforceClient := newTestClient(t, ctx, server.URL, nil)
	users, _, _, err := salesforceClient.GetUsers(ctx, "", 100, true, false)
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Nil(t, users[0].AdditionalFields)
}

func TestAdditionalFieldValues(t *testing.T) {
	ctx := context.Background()

	record := map[string]any{
		"Id":                   "0051X",
		"Role_Based_Access__c": "Tier 2 Support",
		"Is_Contractor__c":     true,
		"Seat_Count__c":        float64(3),
		"Empty__c":             "",
		"Unset__c":             nil,
		"Manager__r":           map[string]any{"Name": "Ada"},
	}

	values := additionalFieldValues(ctx, record, []string{
		"Role_Based_Access__c",
		"Is_Contractor__c",
		"Seat_Count__c",
		"Empty__c",
		"Unset__c",
		"Manager__r",
		"Missing__c",
	})

	require.Equal(t, map[string]any{
		"Role_Based_Access__c": "Tier 2 Support",
		"Is_Contractor__c":     true,
		"Seat_Count__c":        float64(3),
	}, values)
}

func TestAdditionalFieldValuesEmpty(t *testing.T) {
	ctx := context.Background()
	require.Nil(t, additionalFieldValues(ctx, map[string]any{"Id": "0051X"}, nil))
	require.Nil(t, additionalFieldValues(ctx, map[string]any{"Id": "0051X"}, []string{"Missing__c"}))
}
