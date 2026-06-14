package connector

import (
	"context"
	"testing"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	"github.com/conductorone/baton-salesforce/test"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/entitlement"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/stretchr/testify/require"
)

func TestPermissionsGrantsPhase2PSGComponents(t *testing.T) {
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
	c := newPermissionBuilder(salesforceClient)

	t.Run("should return PSG component grants with GrantExpandable annotation in phase 2", func(t *testing.T) {
		permission, _ := permissionResource(&client.SalesforcePermission{ID: "PS2X"})

		grants := make([]*v2.Grant, 0)
		pToken := pagination.Token{Token: "", Size: 100}
		for {
			nextGrants, results, err := c.Grants(ctx, permission, rs.SyncOpAttrs{PageToken: pToken})
			grants = append(grants, nextGrants...)
			require.Nil(t, err)
			require.NotNil(t, results)
			test.AssertNoRatelimitAnnotations(t, results.Annotations)
			if results.NextPageToken == "" {
				break
			}
			pToken.Token = results.NextPageToken
		}

		// Expect one PSG component grant for PSG1X
		require.Len(t, grants, 1)
		require.Equal(t, resourceTypePermissionSetGroup.Id, grants[0].Principal.Id.ResourceType)
		require.Equal(t, "PSG1X", grants[0].Principal.Id.Resource)

		// Verify GrantExpandable annotation is present
		var expandable v2.GrantExpandable
		found, err := test.UnmarshalFromAnys(&expandable, grants[0].Annotations)
		require.Nil(t, err)
		require.True(t, found, "expected GrantExpandable annotation on PSG component grant")
		require.Len(t, expandable.EntitlementIds, 1)
	})
}

func TestPermissionsList(t *testing.T) {
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
	c := newPermissionBuilder(salesforceClient)

	t.Run("should get permissions with pagination", func(t *testing.T) {
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
		require.Len(t, resources, 2)
		require.NotEmpty(t, resources[0].Id)
	})

	t.Run("should grant and revoke entitlements", func(t *testing.T) {
		permission, _ := permissionResource(&client.SalesforcePermission{ID: "345X"})
		user, _ := userResource(ctx, &client.SalesforceUser{ID: "0052X"}, nil, false, false)

		entitlement := v2.Entitlement{
			Id:       entitlement.NewEntitlementID(permission, permissionSetAssignmentEntitlementName),
			Resource: permission,
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
			nextGrants, results, err := c.Grants(ctx, permission, rs.SyncOpAttrs{PageToken: pToken})
			grantsBefore = append(grantsBefore, nextGrants...)

			require.Nil(t, err)
			require.NotNil(t, results)
			test.AssertNoRatelimitAnnotations(t, results.Annotations)
			if results.NextPageToken == "" {
				break
			}
			pToken.Token = results.NextPageToken
		}
		require.Len(t, grantsBefore, 2)

		grant := v2.Grant{
			Entitlement: &entitlement,
			Principal:   user,
		}

		revokeAnnotations, err := c.Revoke(ctx, &grant)
		require.Nil(t, err)
		test.AssertNoRatelimitAnnotations(t, revokeAnnotations)

		if err := uhttp.ClearCaches(ctx); err != nil {
			t.Fatal(err)
		}
		grantsAfter := make([]*v2.Grant, 0)
		pTokenAfter := pagination.Token{
			Token: "",
			Size:  100,
		}
		for {
			nextGrants, results, err := c.Grants(ctx, permission, rs.SyncOpAttrs{PageToken: pTokenAfter})
			grantsAfter = append(grantsAfter, nextGrants...)
			require.Nil(t, err)
			require.NotNil(t, results)
			test.AssertNoRatelimitAnnotations(t, results.Annotations)
			if results.NextPageToken == "" {
				break
			}
			pTokenAfter.Token = results.NextPageToken
		}
		require.Len(t, grantsAfter, 1)
	})
}
