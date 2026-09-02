package connector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	"github.com/conductorone/baton-salesforce/test"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
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

// TestUserResourceAdditionalFields pins how configured extra Salesforce fields
// reach C1: they land in the user profile under their field API name, which is
// what an access review maps onto an attribute.
func TestUserResourceAdditionalFields(t *testing.T) {
	ctx := context.Background()

	t.Run("additional fields land in the profile", func(t *testing.T) {
		resource, err := userResource(
			ctx,
			&client.SalesforceUser{
				ID:        "0051X",
				Username:  "user@example.com",
				Email:     "user@example.com",
				FirstName: "Ada",
				LastName:  "Lovelace",
				UserType:  "Standard",
				IsActive:  true,
				AdditionalFields: map[string]any{
					"Role_Based_Access__c": "Tier 2 Support",
					"Is_Contractor__c":     true,
				},
			},
			nil,
			false,
		)
		require.NoError(t, err)

		profile := resource.GetProfile().AsMap()
		require.Equal(t, "Tier 2 Support", profile["Role_Based_Access__c"])
		require.Equal(t, true, profile["Is_Contractor__c"])
		// The standard keys are untouched.
		require.Equal(t, "Ada Lovelace", profile["full_name"])
		require.Equal(t, "user@example.com", profile["email"])
	})

	t.Run("additional fields never overwrite a standard profile key", func(t *testing.T) {
		resource, err := userResource(
			ctx,
			&client.SalesforceUser{
				ID:               "0051X",
				Username:         "user@example.com",
				Email:            "user@example.com",
				UserType:         "Standard",
				AdditionalFields: map[string]any{"email": "spoofed@example.com"},
			},
			nil,
			false,
		)
		require.NoError(t, err)

		require.Equal(t, "user@example.com", resource.GetProfile().AsMap()["email"])
	})

	t.Run("no additional fields leaves the profile as-is", func(t *testing.T) {
		resource, err := userResource(
			ctx,
			&client.SalesforceUser{
				ID:       "0051X",
				Username: "user@example.com",
				Email:    "user@example.com",
				UserType: "Standard",
			},
			nil,
			false,
		)
		require.NoError(t, err)

		require.Len(t, resource.GetProfile().AsMap(), 5)
	})
}

// TestUserResourceEmitsProfileAndStatusAtBothLevels pins the backwards-compat
// contract around the deprecated UserTrait fields. Profile and status moved to
// the resource in baton-sdk v0.25.0, but the SDK only mirrors trait -> resource,
// never the reverse — so setting only the resource-level options silently empties
// UserTrait.Profile and lets NewUserTrait's ENABLED default stand in for a
// deactivated user's real status. Both levels must agree.
//
//nolint:staticcheck // intentionally reads the deprecated trait profile/status to pin backwards compatibility
func TestUserResourceEmitsProfileAndStatusAtBothLevels(t *testing.T) {
	ctx := context.Background()

	userTraitOf := func(t *testing.T, resource *v2.Resource) *v2.UserTrait {
		t.Helper()

		trait := &v2.UserTrait{}
		annos := annotations.Annotations(resource.GetAnnotations())
		picked, err := annos.Pick(trait)
		require.NoError(t, err)
		require.True(t, picked, "resource carries no user trait")
		return trait
	}

	t.Run("active user", func(t *testing.T) {
		resource, err := userResource(
			ctx,
			&client.SalesforceUser{
				ID:               "0051X",
				Username:         "user@example.com",
				Email:            "user@example.com",
				FirstName:        "Ada",
				LastName:         "Lovelace",
				UserType:         "Standard",
				IsActive:         true,
				AdditionalFields: map[string]any{"Role_Based_Access__c": "Tier 2 Support"},
			},
			nil,
			false,
		)
		require.NoError(t, err)

		trait := userTraitOf(t, resource)
		require.Equal(t, v2.Status_RESOURCE_STATUS_ENABLED, resource.GetStatus().GetStatus())
		require.Equal(t, v2.UserTrait_Status_STATUS_ENABLED, trait.GetStatus().GetStatus())
		require.Equal(t, resource.GetProfile().AsMap(), trait.GetProfile().AsMap())
		require.Equal(t, "Tier 2 Support", trait.GetProfile().AsMap()["Role_Based_Access__c"])

		// rs.GetProfile / rs.GetStatus are the accessors SDK consumers go
		// through (resource_attrs.go); they read the resource level and fall
		// back to the trait. Assert them too, so the test covers what is read
		// and not only what is written.
		require.Equal(t, "Tier 2 Support", rs.GetProfile(resource).AsMap()["Role_Based_Access__c"])
		require.Equal(t, v2.Status_RESOURCE_STATUS_ENABLED, rs.GetStatus(resource).GetStatus())
	})

	// The regression this guards: with only WithResourceStatus, NewUserTrait
	// defaults the unset trait status to ENABLED, reporting a deactivated user
	// as active to anything still reading the trait.
	t.Run("deactivated user", func(t *testing.T) {
		resource, err := userResource(
			ctx,
			&client.SalesforceUser{
				ID:       "0051X",
				Username: "user@example.com",
				Email:    "user@example.com",
				UserType: "Standard",
				IsActive: false,
			},
			nil,
			false,
		)
		require.NoError(t, err)

		trait := userTraitOf(t, resource)
		require.Equal(t, v2.Status_RESOURCE_STATUS_DISABLED, resource.GetStatus().GetStatus())
		require.Equal(t, v2.UserTrait_Status_STATUS_DISABLED, trait.GetStatus().GetStatus())
		require.Equal(t, resource.GetProfile().AsMap(), trait.GetProfile().AsMap())
		require.Equal(t, v2.Status_RESOURCE_STATUS_DISABLED, rs.GetStatus(resource).GetStatus())
	})
}
