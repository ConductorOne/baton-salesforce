package connector

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/conductorone/baton-salesforce/test"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/session"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-sdk/pkg/types/sessions"
	"github.com/stretchr/testify/require"
)

func TestUsersList(t *testing.T) {
	ctx := context.Background()

	t.Run("should get users with pagination", func(t *testing.T) {
		server, db, err := test.FixturesServer(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer test.TearDownDB(ctx, db)
		defer server.Close()

		salesforceClient, err := test.Client(ctx, server.URL)
		if err != nil {
			t.Fatal(err)
		}
		c := newUserBuilder(salesforceClient, false, true, false)

		resources := make([]*v2.Resource, 0)
		pToken := pagination.Token{
			Token: "",
			Size:  1,
		}
		for {
			nextResources, results, err := c.List(ctx, nil, rs.SyncOpAttrs{PageToken: pToken})
			resources = append(resources, nextResources...)

			require.Nil(t, err)
			require.NotNil(t, results)
			test.AssertNoRatelimitAnnotations(t, results.Annotations)
			if results.NextPageToken == "" {
				break
			}

			pToken.Token = results.NextPageToken
		}

		require.NotNil(t, resources)
		require.Len(t, resources, 3)
		require.NotEmpty(t, resources[0].Id)
	})

	t.Run("should classify the agent runtime user as SERVICE", func(t *testing.T) {
		server, db, err := test.FixturesServer(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer test.TearDownDB(ctx, db)
		defer server.Close()

		salesforceClient, err := test.Client(ctx, server.URL)
		if err != nil {
			t.Fatal(err)
		}
		c := newUserBuilder(salesforceClient, false, true, false)
		ss := newMapSessionStore()

		// Drive both phases via the SDK page token: the agents phase records the
		// agent runtime users in the session store, the users phase classifies them.
		resources := make([]*v2.Resource, 0)
		token := ""
		for {
			page, results, err := c.List(ctx, nil, rs.SyncOpAttrs{
				Session:   ss,
				PageToken: pagination.Token{Token: token, Size: 100},
			})
			require.NoError(t, err)
			require.NotNil(t, results)
			resources = append(resources, page...)
			if results.NextPageToken == "" {
				break
			}
			token = results.NextPageToken
		}
		require.Len(t, resources, 3)

		byID := make(map[string]*v2.Resource, len(resources))
		for _, r := range resources {
			byID[r.Id.Resource] = r
		}

		// 0051X is referenced by BotDefinition.BotUserId, so it is the runtime
		// user of an agent and must be classified SERVICE despite UserType
		// "Standard".
		agentUser := byID["0051X"]
		require.NotNil(t, agentUser)
		agentTrait, err := rs.GetUserTrait(agentUser)
		require.NoError(t, err)
		require.Equal(t, v2.UserTrait_ACCOUNT_TYPE_SERVICE, agentTrait.GetAccountType())

		// 0052X is an ordinary Standard user → HUMAN.
		human := byID["0052X"]
		require.NotNil(t, human)
		humanTrait, err := rs.GetUserTrait(human)
		require.NoError(t, err)
		require.Equal(t, v2.UserTrait_ACCOUNT_TYPE_HUMAN, humanTrait.GetAccountType())
	})
}

// TestGetBotDefinitionsGracefulSkip verifies that an org without Agentforce or
// Einstein Bots — where BotDefinition does not exist and Salesforce returns
// INVALID_TYPE — yields no agents and no error, so the agents phase degrades to
// "no agent runtime users" instead of failing the user sync.
func TestGetBotDefinitionsGracefulSkip(t *testing.T) {
	ctx := context.Background()

	const invalidTypeBody = `[{"message":"sObject type 'BotDefinition' is not supported.","errorCode":"INVALID_TYPE"}]`

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

	bots, nextToken, _, err := salesforceClient.GetBotDefinitions(ctx, "")
	require.NoError(t, err)
	require.Empty(t, bots)
	require.Empty(t, nextToken)
}

// TestListAgentsPhaseErrorPropagates verifies that a real (non-INVALID_TYPE)
// BotDefinition failure during the agents phase is propagated from List, so the
// SDK retries from the checkpointed cursor instead of silently shipping a partial
// agent set (orgs without Agentforce are handled separately as an empty result).
func TestListAgentsPhaseErrorPropagates(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`[{"message":"Internal server error","errorCode":"SERVER_ERROR"}]`))
	}))
	defer server.Close()

	salesforceClient, err := test.Client(ctx, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	c := newUserBuilder(salesforceClient, false, true, false)

	// First List call with a session store enters the agents phase, which queries
	// BotDefinition; the 5xx must surface as an error, not a silent skip.
	_, _, err = c.List(ctx, nil, rs.SyncOpAttrs{
		Session:   newMapSessionStore(),
		PageToken: pagination.Token{Token: "", Size: 100},
	})
	require.Error(t, err)
}

// TestListAgentsPhasePaginates drives the agents phase against a BotDefinition
// endpoint that returns two pages and checks that both pages' BotUserIds get
// recorded in the session store — i.e. the agents phase actually paginates via the
// nextRecordsUrl cursor.
func TestListAgentsPhasePaginates(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/query/") {
			// Page 2: the nextRecordsUrl continuation.
			_, _ = w.Write([]byte(`{"totalSize":2,"done":true,"records":[{"attributes":{"type":"BotDefinition"},"Id":"0Xx2","BotUserId":"0052X"}]}`))
			return
		}
		// Page 1, with a cursor to page 2.
		_, _ = w.Write([]byte(`{"totalSize":2,"done":false,"nextRecordsUrl":"/services/data/v60.0/query/01g-1","records":[{"attributes":{"type":"BotDefinition"},"Id":"0Xx1","BotUserId":"0051X"}]}`))
	}))
	defer server.Close()

	salesforceClient, err := test.Client(ctx, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	c := newUserBuilder(salesforceClient, false, true, false)
	ss := newMapSessionStore()

	// Drive only the agents phase: stop once the bag moves on to the users phase.
	token := ""
	calls := 0
	for {
		_, results, err := c.List(ctx, nil, rs.SyncOpAttrs{
			Session:   ss,
			PageToken: pagination.Token{Token: token, Size: 1},
		})
		require.NoError(t, err)
		calls++
		token = results.NextPageToken

		checkBag := &pagination.Bag{}
		require.NoError(t, checkBag.Unmarshal(token))
		if checkBag.ResourceTypeID() != agentsPaginationPhase {
			break
		}
	}

	require.Equal(t, 2, calls, "the agents phase should take two pages")

	// Assert via the same prefixed read the users phase uses, not the mock's
	// internal map, so the test survives a prefix-faithful store.
	recorded, err := session.GetManyJSON[bool](ctx, ss, []string{"0051X", "0052X"}, sessions.WithPrefix(agentRuntimeUsersSessionPrefix))
	require.NoError(t, err)
	require.True(t, recorded["0051X"])
	require.True(t, recorded["0052X"])
}

// TestAccountTypeForUser pins the NHI account-type mapping: both the agent-runtime
// user (isAgentUser) and the AutomatedProcess system user are SERVICE; every other
// user is HUMAN.
func TestAccountTypeForUser(t *testing.T) {
	require.Equal(t, v2.UserTrait_ACCOUNT_TYPE_SERVICE, accountTypeForUser("AutomatedProcess", false))
	require.Equal(t, v2.UserTrait_ACCOUNT_TYPE_SERVICE, accountTypeForUser("Standard", true))
	require.Equal(t, v2.UserTrait_ACCOUNT_TYPE_HUMAN, accountTypeForUser("Standard", false))
}

// mapSessionStore is a minimal in-memory sessions.SessionStore for tests. Only
// Get/Set/GetMany/SetMany are exercised by session.GetJSON/SetJSON/GetManyJSON/
// SetManyJSON; the rest are simple stubs. It ignores SessionStoreOptions (e.g.
// WithPrefix), which is fine for tests since reads and writes are symmetric.
type mapSessionStore struct {
	data map[string][]byte
}

func newMapSessionStore() *mapSessionStore {
	return &mapSessionStore{data: make(map[string][]byte)}
}

func (m *mapSessionStore) Get(_ context.Context, key string, _ ...sessions.SessionStoreOption) ([]byte, bool, error) {
	v, ok := m.data[key]
	return v, ok, nil
}

func (m *mapSessionStore) Set(_ context.Context, key string, value []byte, _ ...sessions.SessionStoreOption) error {
	m.data[key] = value
	return nil
}

func (m *mapSessionStore) GetMany(_ context.Context, keys []string, _ ...sessions.SessionStoreOption) (map[string][]byte, []string, error) {
	// The second return is "unprocessed" keys (to retry), not "missing" keys. We
	// process every key in one shot, so it is always empty; keys not present are
	// simply absent from the found map.
	found := make(map[string][]byte)
	for _, k := range keys {
		if v, ok := m.data[k]; ok {
			found[k] = v
		}
	}
	return found, nil, nil
}

func (m *mapSessionStore) SetMany(_ context.Context, values map[string][]byte, _ ...sessions.SessionStoreOption) error {
	maps.Copy(m.data, values)
	return nil
}

func (m *mapSessionStore) Delete(_ context.Context, key string, _ ...sessions.SessionStoreOption) error {
	delete(m.data, key)
	return nil
}

func (m *mapSessionStore) Clear(_ context.Context, _ ...sessions.SessionStoreOption) error {
	m.data = make(map[string][]byte)
	return nil
}

func (m *mapSessionStore) GetAll(_ context.Context, _ string, _ ...sessions.SessionStoreOption) (map[string][]byte, string, error) {
	out := make(map[string][]byte, len(m.data))
	maps.Copy(out, m.data)
	return out, "", nil
}
