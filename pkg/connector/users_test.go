package connector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/conductorone/baton-salesforce/test"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
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

		resources, _, err := c.List(ctx, nil, rs.SyncOpAttrs{PageToken: pagination.Token{Size: 100}})
		require.NoError(t, err)
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

// TestAgentRuntimeUserIDsGracefulSkip verifies that an org without Agentforce or
// Einstein Bots — where BotDefinition does not exist and Salesforce returns
// INVALID_TYPE — yields an empty agent-user set and no error, so user
// classification degrades to UserType-based defaults instead of failing.
func TestAgentRuntimeUserIDsGracefulSkip(t *testing.T) {
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

	agentUserIDs, _, err := salesforceClient.GetAgentRuntimeUserIDs(ctx)
	require.NoError(t, err)
	require.Empty(t, agentUserIDs)
}
