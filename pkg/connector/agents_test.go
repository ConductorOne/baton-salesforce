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

func TestAgentsList(t *testing.T) {
	ctx := context.Background()

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
	c := newAgentBuilder(salesforceClient)

	t.Run("should list agents with pagination", func(t *testing.T) {
		resources := make([]*v2.Resource, 0)
		pToken := pagination.Token{Token: "", Size: 1}
		for {
			nextResources, results, err := c.List(ctx, nil, rs.SyncOpAttrs{PageToken: pToken})
			resources = append(resources, nextResources...)

			require.NoError(t, err)
			require.NotNil(t, results)
			test.AssertNoRatelimitAnnotations(t, results.Annotations)
			if results.NextPageToken == "" {
				break
			}
			pToken.Token = results.NextPageToken
		}

		require.Len(t, resources, 2)
	})

	t.Run("should emit the agent trait with a profile", func(t *testing.T) {
		resources, _, err := c.List(ctx, nil, rs.SyncOpAttrs{PageToken: pagination.Token{Size: 100}})
		require.NoError(t, err)
		require.Len(t, resources, 2)

		var serviceAgent *v2.Resource
		for _, r := range resources {
			if r.Id.Resource == "0Xx000000000001" {
				serviceAgent = r
			}
		}
		require.NotNil(t, serviceAgent)
		require.Equal(t, resourceTypeAgent.Id, serviceAgent.Id.ResourceType)
		require.Equal(t, "Service Agent", serviceAgent.DisplayName)

		agentTrait, err := rs.GetAgentTrait(serviceAgent)
		require.NoError(t, err)
		require.NotNil(t, agentTrait)
		// v1 is discovery-only: status and identity are intentionally unset.
		require.Equal(t, v2.AgentTrait_AGENT_STATUS_UNSPECIFIED, agentTrait.GetStatus())
		require.Nil(t, agentTrait.GetIdentityResourceId())

		profile := agentTrait.GetProfile().AsMap()
		require.Equal(t, "0Xx000000000001", profile["id"])
		require.Equal(t, "Service_Agent", profile["developer_name"])
		require.Equal(t, "Service Agent", profile["master_label"])
	})
}

// TestAgentsGracefulSkip verifies that an org without Agentforce or Einstein
// Bots — where BotDefinition does not exist and Salesforce returns INVALID_TYPE
// — produces an empty agent list instead of failing the sync.
func TestAgentsGracefulSkip(t *testing.T) {
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
	c := newAgentBuilder(salesforceClient)

	resources, results, err := c.List(ctx, nil, rs.SyncOpAttrs{PageToken: pagination.Token{Size: 100}})
	require.NoError(t, err)
	require.NotNil(t, results)
	require.Empty(t, resources)
	require.Empty(t, results.NextPageToken)
}
