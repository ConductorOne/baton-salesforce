package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/oauth2"
)

// userQueryServer is a Salesforce stand-in that records the SOQL it is handed
// and lets a test control the describe and query responses.
type userQueryServer struct {
	mutex sync.Mutex
	// queries holds every SOQL string the client sent, in order.
	queries []string
	// describeCalls counts describe requests. Guarded by mutex like queries:
	// it is written on the handler goroutine and read from the test goroutine.
	describeCalls int
	// paths holds the URL path of every request, in order. Same guard.
	paths []string

	// describe answers the Nth describe request, N being 1-based.
	describe    func(call int) (int, string)
	queryResult func(soql string) (int, string)
}

func (s *userQueryServer) recordedQueries() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return append([]string(nil), s.queries...)
}

func (s *userQueryServer) recordedPaths() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return append([]string(nil), s.paths...)
}

func (s *userQueryServer) recordedDescribeCalls() int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.describeCalls
}

func (s *userQueryServer) start(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set(uhttp.ContentType, "application/json")

		s.mutex.Lock()
		s.paths = append(s.paths, request.URL.Path)
		s.mutex.Unlock()

		if strings.HasSuffix(request.URL.Path, "/describe") {
			s.mutex.Lock()
			s.describeCalls++
			calls := s.describeCalls
			s.mutex.Unlock()

			status, body := s.describe(calls)
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

// logRecorder is a minimal zapcore.Core that keeps the messages written through
// it, so a test can assert on what an operator would actually see. Cheaper than
// vendoring zaptest/observer for one assertion.
type logRecorder struct {
	zapcore.LevelEnabler
	mutex    sync.Mutex
	messages []string
}

func (r *logRecorder) With([]zapcore.Field) zapcore.Core { return r }

func (r *logRecorder) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if r.Enabled(entry.Level) {
		return checked.AddCore(entry, r)
	}
	return checked
}

func (r *logRecorder) Write(entry zapcore.Entry, _ []zapcore.Field) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.messages = append(r.messages, entry.Message)
	return nil
}

func (r *logRecorder) Sync() error { return nil }

func (r *logRecorder) contains(substr string) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for _, message := range r.messages {
		if strings.Contains(message, substr) {
			return true
		}
	}
	return false
}

// recordingContext returns a context whose logger feeds the returned recorder.
func recordingContext() (context.Context, *logRecorder) {
	recorder := &logRecorder{LevelEnabler: zapcore.DebugLevel}
	return ctxzap.ToContext(context.Background(), zap.New(recorder)), recorder
}

func TestNormalizeAdditionalFieldsCapsCount(t *testing.T) {
	const truncationWarning = "too many additional fields configured"

	// Distinct, valid, non-standard field names: Custom_0__c, Custom_1__c, ...
	uniqueFields := func(n int) []string {
		fields := make([]string, 0, n)
		for i := range n {
			fields = append(fields, fmt.Sprintf("Custom_%d__c", i))
		}
		return fields
	}

	// A list exactly at the cap is legal: it comes through whole AND draws no
	// warning. Warning here would tell an operator their config was truncated
	// when nothing was dropped.
	t.Run("exactly at the cap is not truncated", func(t *testing.T) {
		ctx, logs := recordingContext()
		atCap := uniqueFields(maxAdditionalFields)

		require.Equal(t, atCap, NormalizeAdditionalFields(ctx, TableNameUsers, atCap))
		require.False(t, logs.contains(truncationWarning), "warned about truncation with nothing dropped")
	})

	t.Run("over the cap is truncated and warns", func(t *testing.T) {
		ctx, logs := recordingContext()
		overCap := uniqueFields(maxAdditionalFields + 10)

		require.Equal(t, overCap[:maxAdditionalFields], NormalizeAdditionalFields(ctx, TableNameUsers, overCap))
		require.True(t, logs.contains(truncationWarning))
	})
}

// TestGetUsersWithAdditionalFields is the ticket's core case: a configured
// custom picklist is selected and its value reaches the user.
func TestGetUsersWithAdditionalFields(t *testing.T) {
	ctx := context.Background()

	stub := &userQueryServer{
		describe: func(int) (int, string) {
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
		describe: func(int) (int, string) {
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
		describe: func(int) (int, string) {
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

	// For the rest of THIS sync the field stays off, so no later query repeats
	// the failure. GetUserByEmail shares the client's resolution state and does
	// not start a new sync, which makes it the observable stand-in for a later
	// page (a real page two replays a Salesforce-issued URL we can't rebuild).
	require.NoError(t, uhttp.ClearCaches(ctx))
	_, err = salesforceClient.GetUserByEmail(ctx, "user@example.com")
	require.NoError(t, err)
	queries = stub.recordedQueries()
	require.Len(t, queries, 3)
	require.NotContains(t, queries[2], "Role_Based_Access__c")
}

// TestGetUsersReResolvesOnNextSync is the other half of the fallback contract:
// switching the fields off lasts for the sync that hit the error, not for the
// life of the process. A hosted connector runs for days, so a customer who
// fixes a misspelled field name has to see it take effect without a restart.
func TestGetUsersReResolvesOnNextSync(t *testing.T) {
	ctx := context.Background()

	const invalidField = `[{"message":"No such column 'Role_Based_Access__c' on entity 'User'.","errorCode":"INVALID_FIELD"}]`

	// Read on the httptest handler goroutines, written here on the test
	// goroutine — the same cross-goroutine handoff the stub guards describeCalls
	// for. A socket round-trip is not a happens-before edge.
	var describeWorks atomic.Bool
	stub := &userQueryServer{
		describe: func(int) (int, string) {
			if !describeWorks.Load() {
				return http.StatusInternalServerError, `[{"message":"boom","errorCode":"SERVER_ERROR"}]`
			}
			return http.StatusOK, describeResponse(t, "Id", "Role_Based_Access__c")
		},
		queryResult: func(soql string) (int, string) {
			if strings.Contains(soql, "Role_Based_Access__c") && !describeWorks.Load() {
				return http.StatusBadRequest, invalidField
			}
			return http.StatusOK, usersResponse(t, standardUserRecord(map[string]any{
				"Role_Based_Access__c": "Tier 2 Support",
			}))
		},
	}
	server := stub.start(t)

	// Sync one: the field is rejected and switched off for the rest of the sync.
	salesforceClient := newTestClient(t, ctx, server.URL, []string{"Role_Based_Access__c"})
	users, _, _, err := salesforceClient.GetUsers(ctx, "", 100, true, false)
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Nil(t, users[0].AdditionalFields)

	// The org is fixed (field created, or the integration user granted access).
	describeWorks.Store(true)

	// Sync two re-resolves from config without a restart, and the field is back.
	require.NoError(t, uhttp.ClearCaches(ctx))
	users, _, _, err = salesforceClient.GetUsers(ctx, "", 100, true, false)
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(
		t,
		map[string]any{"Role_Based_Access__c": "Tier 2 Support"},
		users[0].AdditionalFields,
	)
	require.Contains(t, stub.recordedQueries()[2], "Role_Based_Access__c")
}

// TestGetUsersWithoutAdditionalFields pins the default: no config, no describe
// call, and the query is unchanged.
func TestGetUsersWithoutAdditionalFields(t *testing.T) {
	ctx := context.Background()

	stub := &userQueryServer{
		describe: func(int) (int, string) {
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

// TestGetUserByEmailWithAdditionalFields covers the provisioning path: a newly
// created account's resource carries the configured fields too.
func TestGetUserByEmailWithAdditionalFields(t *testing.T) {
	ctx := context.Background()

	stub := &userQueryServer{
		describe: func(int) (int, string) {
			return http.StatusOK, describeResponse(t, "Id", "Email", "Role_Based_Access__c")
		},
		queryResult: func(string) (int, string) {
			return http.StatusOK, usersResponse(t, standardUserRecord(map[string]any{
				"Role_Based_Access__c": "Tier 2 Support",
			}))
		},
	}
	server := stub.start(t)

	salesforceClient := newTestClient(t, ctx, server.URL, []string{"Role_Based_Access__c"})
	user, err := salesforceClient.GetUserByEmail(ctx, "user@example.com")
	require.NoError(t, err)
	require.Equal(
		t,
		map[string]any{"Role_Based_Access__c": "Tier 2 Support"},
		user.AdditionalFields,
	)

	queries := stub.recordedQueries()
	require.Len(t, queries, 1)
	require.Contains(t, queries[0], "Role_Based_Access__c")
	require.Contains(t, queries[0], "Email = 'user@example.com'")
}

// TestGetUserByEmailInvalidFieldFallback mirrors TestGetUsersInvalidFieldFallback
// on the provisioning path, where a regression would surface as a failed account
// creation rather than a degraded sync.
func TestGetUserByEmailInvalidFieldFallback(t *testing.T) {
	ctx := context.Background()

	const invalidField = `[{"message":"No such column 'Role_Based_Access__c' on entity 'User'.","errorCode":"INVALID_FIELD"}]`

	stub := &userQueryServer{
		describe: func(int) (int, string) {
			// Describe is unavailable, so the configured names are used as-is and
			// the query-time fallback is the only thing standing between a typo
			// and a failed lookup.
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
	user, err := salesforceClient.GetUserByEmail(ctx, "user@example.com")
	require.NoError(t, err)
	require.NotNil(t, user)
	require.Equal(t, "user@example.com", user.Email)
	require.Nil(t, user.AdditionalFields)

	queries := stub.recordedQueries()
	require.Len(t, queries, 2)
	require.Contains(t, queries[0], "Role_Based_Access__c")
	require.NotContains(t, queries[1], "Role_Based_Access__c")

	// The field stays off until the next full user sync re-opens resolution, so
	// a later lookup doesn't repeat the failure. (Only GetUsers on its first page
	// resets; see disableAdditionalUserFields.)
	require.NoError(t, uhttp.ClearCaches(ctx))
	_, err = salesforceClient.GetUserByEmail(ctx, "other@example.com")
	require.NoError(t, err)
	queries = stub.recordedQueries()
	require.Len(t, queries, 3)
	require.NotContains(t, queries[2], "Role_Based_Access__c")
}

// TestGetUsersRetriesDescribeAfterTransientFailure pins that a flaky describe
// doesn't permanently cost us canonical-casing resolution: the next call tries
// again, and once it succeeds the mis-cased config name is selected canonically.
func TestGetUsersRetriesDescribeAfterTransientFailure(t *testing.T) {
	ctx := context.Background()

	stub := &userQueryServer{
		describe: func(call int) (int, string) {
			if call == 1 {
				return http.StatusInternalServerError, `[{"message":"boom","errorCode":"SERVER_ERROR"}]`
			}
			return http.StatusOK, describeResponse(t, "Id", "Role_Based_Access__c")
		},
		queryResult: func(string) (int, string) {
			return http.StatusOK, usersResponse(t, standardUserRecord(map[string]any{
				"Role_Based_Access__c": "Tier 2 Support",
			}))
		},
	}
	server := stub.start(t)

	// Configured with the wrong casing, which only the describe can correct.
	salesforceClient := newTestClient(t, ctx, server.URL, []string{"role_based_access__c"})

	// First page: describe fails, so the configured casing goes into the SELECT.
	users, _, _, err := salesforceClient.GetUsers(ctx, "", 100, true, false)
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Contains(t, stub.recordedQueries()[0], "role_based_access__c")
	// The value still lands, keyed by the canonical name Salesforce reported.
	require.Equal(
		t,
		map[string]any{"Role_Based_Access__c": "Tier 2 Support"},
		users[0].AdditionalFields,
	)

	// Second page: describe is retried and succeeds, so the canonical name is used.
	require.NoError(t, uhttp.ClearCaches(ctx))
	users, _, _, err = salesforceClient.GetUsers(ctx, "", 100, true, false)
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(t, 2, stub.recordedDescribeCalls())

	queries := stub.recordedQueries()
	require.Len(t, queries, 2)
	require.Contains(t, queries[1], "Role_Based_Access__c")
	require.NotContains(t, queries[1], "role_based_access__c")
}

// TestGetUsersCapsDescribeAttemptsPerSync pins the other side of the retry: a
// describe endpoint that is genuinely broken is retried a bounded number of
// times per sync, not once per call — and the budget re-opens on the next sync.
func TestGetUsersCapsDescribeAttemptsPerSync(t *testing.T) {
	ctx := context.Background()

	stub := &userQueryServer{
		describe: func(int) (int, string) {
			return http.StatusInternalServerError, `[{"message":"boom","errorCode":"SERVER_ERROR"}]`
		},
		// The query succeeds even with the unresolved field, so the INVALID_FIELD
		// fallback never fires and the describe budget is what's under test.
		queryResult: func(string) (int, string) {
			return http.StatusOK, usersResponse(t, standardUserRecord(nil))
		},
	}
	server := stub.start(t)

	salesforceClient := newTestClient(t, ctx, server.URL, []string{"Role_Based_Access__c"})

	// One sync: the first page plus several provisioning lookups, none of which
	// starts a new sync. Only the first maxDescribeAttempts of them describe.
	_, _, _, err := salesforceClient.GetUsers(ctx, "", 100, true, false)
	require.NoError(t, err)
	for range maxDescribeAttempts + 3 {
		require.NoError(t, uhttp.ClearCaches(ctx))
		_, err = salesforceClient.GetUserByEmail(ctx, "user@example.com")
		require.NoError(t, err)
	}
	require.Equal(t, maxDescribeAttempts, stub.recordedDescribeCalls())

	// A new sync re-opens the budget rather than staying given up for the life
	// of the process.
	require.NoError(t, uhttp.ClearCaches(ctx))
	_, _, _, err = salesforceClient.GetUsers(ctx, "", 100, true, false)
	require.NoError(t, err)
	require.Equal(t, maxDescribeAttempts+1, stub.recordedDescribeCalls())
}

// TestAdditionalFieldValuesCaseInsensitive covers the describe-unavailable path:
// SOQL is case-insensitive but the JSON response is not, so a mis-cased config
// name must still find its value — keyed by the name Salesforce reported.
func TestAdditionalFieldValuesCaseInsensitive(t *testing.T) {
	ctx := context.Background()

	record := map[string]any{
		"Id":                   "0051X",
		"Role_Based_Access__c": "Tier 2 Support",
		"Is_Contractor__c":     true,
	}

	values := additionalFieldValues(ctx, record, []string{
		"role_based_access__c",
		"IS_CONTRACTOR__C",
		"Missing__c",
	})

	require.Equal(t, map[string]any{
		"Role_Based_Access__c": "Tier 2 Support",
		"Is_Contractor__c":     true,
	}, values)
}

// TestGetUserByEmailMatchesGetUsers pins that the provisioning lookup and the
// sync build the same SalesforceUser from the same record. LicenseDefinitionKey
// was the gap: it is in the standard SELECT and userResource feeds it to
// accountTypeForUser, so omitting it here classified a user created through
// provisioning as HUMAN while the next sync classified them as SERVICE.
func TestGetUserByEmailMatchesGetUsers(t *testing.T) {
	ctx := context.Background()

	record := standardUserRecord(map[string]any{
		"FirstName": "Ada",
		"LastName":  "Lovelace",
		"Profile": map[string]any{
			"UserLicense": map[string]any{
				"LicenseDefinitionKey": "PID_Integration",
			},
		},
		"Role_Based_Access__c": "Tier 2 Support",
	})

	stub := &userQueryServer{
		describe: func(int) (int, string) {
			return http.StatusOK, describeResponse(t, "Id", "Role_Based_Access__c")
		},
		queryResult: func(string) (int, string) {
			return http.StatusOK, usersResponse(t, record)
		},
	}
	server := stub.start(t)

	salesforceClient := newTestClient(t, ctx, server.URL, []string{"Role_Based_Access__c"})

	synced, _, _, err := salesforceClient.GetUsers(ctx, "", 100, true, false)
	require.NoError(t, err)
	require.Len(t, synced, 1)

	require.NoError(t, uhttp.ClearCaches(ctx))
	provisioned, err := salesforceClient.GetUserByEmail(ctx, "user@example.com")
	require.NoError(t, err)

	require.Equal(t, "PID_Integration", synced[0].LicenseDefinitionKey)
	require.Equal(t, synced[0], provisioned)
}

// TestAdditionalUserFieldsConcurrentResolution exercises the double-checked
// locking around the describe. The describe now runs outside the mutex so a
// slow one can't park every concurrent caller, which means two callers can race
// into it; whoever stores first wins and everyone must observe the same list.
// Meaningful under -race.
func TestAdditionalUserFieldsConcurrentResolution(t *testing.T) {
	ctx := context.Background()

	stub := &userQueryServer{
		describe: func(int) (int, string) {
			return http.StatusOK, describeResponse(t, "Id", "Role_Based_Access__c")
		},
		queryResult: func(string) (int, string) {
			return http.StatusOK, usersResponse(t, standardUserRecord(map[string]any{
				"Role_Based_Access__c": "Tier 2 Support",
			}))
		},
	}
	server := stub.start(t)

	salesforceClient := newTestClient(t, ctx, server.URL, []string{"Role_Based_Access__c"})

	const callers = 8
	results := make([][]string, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = salesforceClient.additionalUserFieldNames(ctx)
		}()
	}
	wg.Wait()

	for i, got := range results {
		require.Equal(t, []string{"Role_Based_Access__c"}, got, "caller %d", i)
	}
}

// TestDescribeUsesTheSameAPIVersionAsTheQuery guards the layering: the describe
// check only keeps bad names out of the SELECT if it is describing the same
// schema the SELECT will run against. Describing a newer version would admit a
// field that exists only there, and Salesforce would then reject the whole
// query with INVALID_FIELD — dropping every configured field for that sync,
// which is the failure the describe check exists to prevent.
func TestDescribeUsesTheSameAPIVersionAsTheQuery(t *testing.T) {
	ctx := context.Background()

	stub := &userQueryServer{
		describe: func(int) (int, string) {
			return http.StatusOK, describeResponse(t, "Id", "Role_Based_Access__c")
		},
		queryResult: func(string) (int, string) {
			return http.StatusOK, usersResponse(t, standardUserRecord(nil))
		},
	}
	server := stub.start(t)

	salesforceClient := newTestClient(t, ctx, server.URL, []string{"Role_Based_Access__c"})
	_, _, _, err := salesforceClient.GetUsers(ctx, "", 100, true, false)
	require.NoError(t, err)

	// Compare the versions the two requests actually used rather than asserting
	// a literal, so bumping simpleforce keeps this honest instead of stale.
	// Segments are matched by position after "data" rather than by index:
	// ApexREST joins instanceURL and path with a "/" that the path already has,
	// so a describe URL carries a leading "//".
	apiVersion := func(path string) string {
		segments := strings.Split(path, "/")
		for i, segment := range segments {
			if segment == "data" && i+1 < len(segments) {
				return segments[i+1]
			}
		}
		require.Failf(t, "no API version in path", "path %q", path)
		return ""
	}

	var describeVersion, queryVersion string
	for _, path := range stub.recordedPaths() {
		switch {
		case strings.HasSuffix(path, "/describe"):
			describeVersion = apiVersion(path)
		case strings.HasSuffix(path, "/query"):
			queryVersion = apiVersion(path)
		}
	}

	require.NotEmpty(t, describeVersion, "no describe request recorded")
	require.NotEmpty(t, queryVersion, "no query request recorded")
	require.Equal(t, queryVersion, describeVersion,
		"describe validates against a different API version than the SOQL runs at")
}

// TestInitializeIsConcurrencySafe covers the setup this branch made reachable
// from more than one goroutine: Initialize writes client, salesforceTransport
// and initialized together, and a torn init leaves a caller using a client whose
// transport is not the one currentRateLimit reads. Meaningful under -race.
func TestInitializeIsConcurrencySafe(t *testing.T) {
	ctx := context.Background()

	stub := &userQueryServer{
		describe: func(int) (int, string) {
			return http.StatusOK, describeResponse(t, "Id")
		},
		queryResult: func(string) (int, string) {
			return http.StatusOK, usersResponse(t, standardUserRecord(nil))
		},
	}
	server := stub.start(t)

	require.NoError(t, uhttp.ClearCaches(ctx))
	// Deliberately NOT pre-initialized, unlike newTestClient.
	salesforceClient := New(
		server.URL,
		oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "mock-access-token"}),
		"",
		"",
		"",
	)

	const callers = 8
	var wg sync.WaitGroup
	errs := make([]error, callers)
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = salesforceClient.Initialize(ctx)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "caller %d", i)
	}
	require.NotNil(t, salesforceClient.client)
	require.NotNil(t, salesforceClient.salesforceTransport)
}

// TestInvalidFieldLogsErrorForNonRecoveringCallers pins the scope of the
// INVALID_FIELD log downgrade. The recovering User queries log it at Debug —
// they retry and report the recovery themselves — but every other caller, and
// the standard-fields retry itself, must keep the Error line, since it is the
// only log carrying the offending SOQL.
func TestInvalidFieldLogsErrorForNonRecoveringCallers(t *testing.T) {
	const invalidField = `[{"message":"No such column 'Nope__c' on entity 'User'.","errorCode":"INVALID_FIELD"}]`
	const errorMessage = "error querying salesforce"
	const debugMessage = "salesforce rejected a selected field"

	newClient := func(t *testing.T, ctx context.Context, fields []string) *SalesforceClient {
		t.Helper()

		stub := &userQueryServer{
			describe: func(int) (int, string) {
				return http.StatusOK, describeResponse(t, "Id", "Nope__c")
			},
			queryResult: func(string) (int, string) {
				return http.StatusBadRequest, invalidField
			},
		}
		return newTestClient(t, ctx, stub.start(t).URL, fields)
	}

	t.Run("recovering user query logs at debug", func(t *testing.T) {
		ctx, logs := recordingContext()
		salesforceClient := newClient(t, ctx, []string{"Nope__c"})

		// Both attempts fail here: the first with the extra field (tolerated,
		// Debug), the retry with the standard set (real failure, Error).
		_, _, _, err := salesforceClient.GetUsers(ctx, "", 100, true, false)
		require.Error(t, err)
		require.True(t, logs.contains(debugMessage), "recoverable rejection was not logged at debug")
		require.True(t, logs.contains(errorMessage), "the standard-fields retry lost its error log")
	})

	t.Run("other callers keep the error log", func(t *testing.T) {
		ctx, logs := recordingContext()
		// No additional fields configured, so nothing can recover.
		salesforceClient := newClient(t, ctx, nil)

		_, _, _, err := salesforceClient.GetUsers(ctx, "", 100, true, false)
		require.Error(t, err)
		require.True(t, logs.contains(errorMessage), "a non-recoverable rejection lost its error log")
		require.False(t, logs.contains(debugMessage))
	})
}

// TestAdditionalFieldValuesLargeNumberPrecision pins a known limitation rather
// than asserting desired behaviour. Salesforce Number and Auto Number fields
// allow 18 digits, but simpleforce decodes records with json.Unmarshal and no
// UseNumber (force.go:97), so anything past float64's exact-integer range is
// already rounded before it reaches us. Rendering it as a string here would not
// recover the digits — only disguise the rounding — so it passes through as a
// number and docs/connector.mdx tells operators to use a Text field instead.
func TestAdditionalFieldValuesLargeNumberPrecision(t *testing.T) {
	ctx := context.Background()

	var record map[string]any
	require.NoError(t, json.Unmarshal(
		[]byte(`{"Id":"0051X","Employee_ID__c":123456789012345678,"Seat_Count__c":42}`),
		&record,
	))

	values := additionalFieldValues(ctx, record, []string{"Employee_ID__c", "Seat_Count__c"})

	// Rounded on the way in, not by us: the exact value never survives the decode.
	require.Equal(t, float64(123456789012345678), values["Employee_ID__c"])
	require.NotEqual(t, "123456789012345678",
		strconv.FormatFloat(values["Employee_ID__c"].(float64), 'f', -1, 64),
		"if this passes, simpleforce started preserving integer precision and the docs note can go")

	// Values inside 2^53 — the overwhelmingly common case — are exact.
	require.Equal(t, float64(42), values["Seat_Count__c"])
}

// TestGetUsersDoesNotReResolveMidSync guards against drift between the SELECT
// and the extraction list. Page two replays a Salesforce-issued nextRecordsUrl
// whose SELECT was frozen on page one, so if page one ran with the raw
// configured names (describe down) and page two re-resolved successfully and
// dropped one, that field would silently vanish from page-two profiles while
// page-one users kept it.
func TestGetUsersDoesNotReResolveMidSync(t *testing.T) {
	ctx := context.Background()

	var describeWorks atomic.Bool
	stub := &userQueryServer{
		describe: func(int) (int, string) {
			if !describeWorks.Load() {
				return http.StatusInternalServerError, `[{"message":"boom","errorCode":"SERVER_ERROR"}]`
			}
			// Would drop Role_Based_Access__c if page two were allowed to re-resolve.
			return http.StatusOK, describeResponse(t, "Id", "Username")
		},
		queryResult: func(string) (int, string) {
			return http.StatusOK, usersResponse(t, standardUserRecord(map[string]any{
				"Role_Based_Access__c": "Tier 2 Support",
			}))
		},
	}
	server := stub.start(t)

	salesforceClient := newTestClient(t, ctx, server.URL, []string{"Role_Based_Access__c"})

	// Page one: describe is down, so the configured name goes into the SELECT.
	firstPage, _, _, err := salesforceClient.GetUsers(ctx, "", 100, true, false)
	require.NoError(t, err)
	require.Len(t, firstPage, 1)
	require.Equal(t, "Tier 2 Support", firstPage[0].AdditionalFields["Role_Based_Access__c"])

	describeCallsAfterPageOne := stub.recordedDescribeCalls()

	// Page two, same sync. The describe is healthy again and would now drop the
	// field — but the SELECT is already fixed, so the field must still be read.
	describeWorks.Store(true)
	require.NoError(t, uhttp.ClearCaches(ctx))
	secondPage, _, _, err := salesforceClient.GetUsers(ctx, "/services/data/v54.0/query/01g-2000", 100, true, false)
	require.NoError(t, err)
	require.Len(t, secondPage, 1)
	require.Equal(t, firstPage[0].AdditionalFields, secondPage[0].AdditionalFields,
		"page two extracted a different field set than page one selected")

	require.Equal(t, describeCallsAfterPageOne, stub.recordedDescribeCalls(),
		"page two re-resolved; the SELECT it replays was fixed on page one")
}
