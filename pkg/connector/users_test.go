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
}

// TestGetBotDefinitionsGracefulSkip verifies that an org without Agentforce or
// Einstein Bots — where BotDefinition does not exist and Salesforce returns
// INVALID_TYPE — yields no agents and no error, so the agent syncer degrades to
// "no agents" instead of failing the sync.
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

// TestAccountTypeForUser pins the NHI account-type mapping. SERVICE is driven by
// immutable system signals: UserType (AutomatedProcess / CloudIntegrationUser) and
// the Agentforce license key (PID_DigitalAgent). Everything else — including a human
// reused as a bot's running user (Standard + SFDC) — is HUMAN.
func TestAccountTypeForUser(t *testing.T) {
	cases := []struct {
		name       string
		userType   string
		licenseKey string
		want       v2.UserTrait_AccountType
	}{
		// SERVICE — UserType signals
		{"automated process", "AutomatedProcess", "", v2.UserTrait_ACCOUNT_TYPE_SERVICE},
		{"cloud integration (einstein bot runtime)", "CloudIntegrationUser", "", v2.UserTrait_ACCOUNT_TYPE_SERVICE},
		// SERVICE — agent license prefix (Einstein Agent + External Einstein Agent)
		{"einstein agent", "Standard", "PID_DigitalAgent", v2.UserTrait_ACCOUNT_TYPE_SERVICE},
		{"external einstein agent", "Standard", "PID_DigitalAgentExternal", v2.UserTrait_ACCOUNT_TYPE_SERVICE},
		// SERVICE — integration license suffix
		{"salesforce integration", "Standard", "SALESFORCE_INTEGRATION_USER", v2.UserTrait_ACCOUNT_TYPE_SERVICE},
		{"crm analytics integration", "Standard", "INSIGHTS_INTEGRATION_USER", v2.UserTrait_ACCOUNT_TYPE_SERVICE},
		{"cloud integration license", "Standard", "CLOUD_INTEGRATION_USER", v2.UserTrait_ACCOUNT_TYPE_SERVICE},
		// SERVICE — cross-org proxy system user
		{"xorg proxy", "Standard", "PID_XOrg_Proxy_User", v2.UserTrait_ACCOUNT_TYPE_SERVICE},
		// HUMAN — people (internal + external), must not match the patterns
		{"standard human", "Standard", "SFDC", v2.UserTrait_ACCOUNT_TYPE_HUMAN},
		{"human reused as bot user", "Standard", "SFDC", v2.UserTrait_ACCOUNT_TYPE_HUMAN},
		{"platform human", "Standard", "AUL", v2.UserTrait_ACCOUNT_TYPE_HUMAN},
		{"customer community (external person)", "CspLitePortal", "PID_Customer_Community", v2.UserTrait_ACCOUNT_TYPE_HUMAN},
		{"partner community (external person)", "PowerPartner", "PID_Partner_Community", v2.UserTrait_ACCOUNT_TYPE_HUMAN},
		{"chatter free", "CsnOnly", "CSN_User", v2.UserTrait_ACCOUNT_TYPE_HUMAN},
		{"standard no license", "Standard", "", v2.UserTrait_ACCOUNT_TYPE_HUMAN},
		// Pattern false-positive guards: "integration"/"digitalagent" appear but not as
		// the exact suffix/prefix, so these must stay HUMAN (not a substring match).
		{"integration substring, not the suffix", "Standard", "PID_Customer_Integration_Login", v2.UserTrait_ACCOUNT_TYPE_HUMAN},
		{"digitalagent substring, not the prefix", "Standard", "PID_Custom_DigitalAgent", v2.UserTrait_ACCOUNT_TYPE_HUMAN},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, accountTypeForUser(tc.userType, tc.licenseKey))
		})
	}
}
