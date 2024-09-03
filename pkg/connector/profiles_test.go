package connector

import (
	"context"
	"testing"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	"github.com/conductorone/baton-salesforce/test"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/stretchr/testify/require"
)

func TestProfilesList(t *testing.T) {
	ctx := context.Background()

	server, db, err := test.FixturesServer()
	if err != nil {
		t.Fatal(err)
	}
	defer test.TearDownDB(db)
	defer server.Close()

	confluenceClient := client.New(server.URL, test.MockTokenSource(), "", "")
	err = confluenceClient.Initialize(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := newProfileBuilder(confluenceClient)

	t.Run("should get profiles with pagination", func(t *testing.T) {
		resources := make([]*v2.Resource, 0)
		pToken := pagination.Token{
			Token: "",
			Size:  1,
		}
		for {
			nextResources, nextToken, listAnnotations, err := c.List(ctx, nil, &pToken)
			resources = append(resources, nextResources...)

			require.Nil(t, err)
			test.AssertNoRatelimitAnnotations(t, listAnnotations)
			if nextToken == "" {
				break
			}

			pToken.Token = nextToken
		}

		require.NotNil(t, resources)
		require.Len(t, resources, 2)
		require.NotEmpty(t, resources[0].Id)
	})

	t.Run("should grant and revoke entitlements", func(t *testing.T) {
		profile, _ := profileResource(ctx, &client.SalesforceProfile{ID: "198X"})
		user, _ := userResource(ctx, &client.SalesforceUser{ID: "0052X"}, false)

		entitlement := v2.Entitlement{
			Id:       entitlement.NewEntitlementID(profile, profileAssignmentEntitlementName),
			Resource: profile,
		}

		grantAnnotations, err := c.Grant(ctx, user, &entitlement)
		require.Nil(t, err)
		test.AssertNoRatelimitAnnotations(t, grantAnnotations)

		grantsBefore := make([]*v2.Grant, 0)
		pToken := pagination.Token{
			Token: "",
			Size:  100,
		}
		for {
			nextGrants, nextToken, listAnnotations, err := c.Grants(ctx, profile, &pToken)
			grantsBefore = append(grantsBefore, nextGrants...)

			require.Nil(t, err)
			test.AssertNoRatelimitAnnotations(t, listAnnotations)
			if nextToken == "" {
				break
			}
			pToken.Token = nextToken
		}
		require.Len(t, grantsBefore, 1)

		grant := v2.Grant{
			Entitlement: &entitlement,
			Principal:   user,
		}

		revokeAnnotations, err := c.Revoke(ctx, &grant)
		require.Nil(t, err)
		test.AssertNoRatelimitAnnotations(t, revokeAnnotations)

		grantsAfter, nextToken, grantsAnnotations, err := c.Grants(ctx, profile, &pagination.Token{})
		require.Nil(t, err)
		test.AssertNoRatelimitAnnotations(t, grantsAnnotations)
		require.Equal(t, "", nextToken)
		require.Len(t, grantsAfter, 0)
	})
}
