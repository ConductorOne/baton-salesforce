package connector

import (
	"context"
	"testing"

	"github.com/conductorone/baton-salesforce/test"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	sdkEnt "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/stretchr/testify/require"
)

func TestTerritoriesList(t *testing.T) {
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
	c := newTerritoryBuilder(salesforceClient)

	t.Run("should list territories with pagination", func(t *testing.T) {
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
		require.NotEmpty(t, resources[0].Id)
	})

	t.Run("should return member and role static entitlements", func(t *testing.T) {
		entitlements, results, err := c.StaticEntitlements(ctx, rs.SyncOpAttrs{})
		require.NoError(t, err)
		require.NotNil(t, results)
		test.AssertNoRatelimitAnnotations(t, results.Annotations)

		// member + role:Owner + role:Sales Rep
		require.Len(t, entitlements, 3)

		slugs := make(map[string]bool)
		for _, e := range entitlements {
			slugs[e.Slug] = true
		}
		require.True(t, slugs[territoryMemberPermission])
		require.True(t, slugs[territoryRolePermissionPrefix+"Owner"])
		require.True(t, slugs[territoryRolePermissionPrefix+"Sales Rep"])
	})

	t.Run("should return grants for a territory", func(t *testing.T) {
		territory := &v2.Resource{
			Id:          &v2.ResourceId{ResourceType: resourceTypeTerritory.Id, Resource: "T1"},
			DisplayName: "Argentina",
		}

		grants := make([]*v2.Grant, 0)
		pToken := pagination.Token{Token: "", Size: 100}
		for {
			nextGrants, results, err := c.Grants(ctx, territory, rs.SyncOpAttrs{PageToken: pToken})
			grants = append(grants, nextGrants...)

			require.NoError(t, err)
			require.NotNil(t, results)
			test.AssertNoRatelimitAnnotations(t, results.Annotations)
			if results.NextPageToken == "" {
				break
			}
			pToken.Token = results.NextPageToken
		}

		require.Len(t, grants, 4)
	})
}

func TestTerritoriesProvisioning(t *testing.T) {
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
	c := newTerritoryBuilder(salesforceClient)

	// T2 (Brasil) starts with one member: 0051X
	territory := &v2.Resource{
		Id:          &v2.ResourceId{ResourceType: resourceTypeTerritory.Id, Resource: "T2"},
		DisplayName: "Brasil",
	}
	ent := &v2.Entitlement{
		Id:       sdkEnt.NewEntitlementID(territory, territoryMemberPermission),
		Resource: territory,
		Slug:     territoryMemberPermission,
	}

	t.Run("should grant territory membership", func(t *testing.T) {
		principal := &v2.Resource{
			Id: &v2.ResourceId{ResourceType: resourceTypeUser.Id, Resource: "0052X"},
		}

		_, annos, err := c.Grant(ctx, principal, ent)
		require.NoError(t, err)
		test.AssertNoRatelimitAnnotations(t, annos)

		grants, _, err := c.Grants(ctx, territory, rs.SyncOpAttrs{PageToken: pagination.Token{Size: 100}})
		require.NoError(t, err)
		userIDs := make(map[string]bool)
		for _, g := range grants {
			userIDs[g.Principal.Id.Resource] = true
		}
		require.True(t, userIDs["0052X"])
	})

	t.Run("should handle idempotent grant", func(t *testing.T) {
		// 0051X is already a member of T2
		principal := &v2.Resource{
			Id: &v2.ResourceId{ResourceType: resourceTypeUser.Id, Resource: "0051X"},
		}

		grants, annos, err := c.Grant(ctx, principal, ent)
		require.NoError(t, err)
		require.Empty(t, grants)
		test.AssertNoRatelimitAnnotations(t, annos)
		test.AssertContainsAnnotation(t, &v2.GrantAlreadyExists{}, annos)
	})

	t.Run("should revoke territory membership", func(t *testing.T) {
		principal := &v2.Resource{
			Id: &v2.ResourceId{ResourceType: resourceTypeUser.Id, Resource: "0051X"},
		}

		g := &v2.Grant{
			Principal:   principal,
			Entitlement: ent,
		}

		annos, err := c.Revoke(ctx, g)
		require.NoError(t, err)
		test.AssertNoRatelimitAnnotations(t, annos)

		if err := uhttp.ClearCaches(ctx); err != nil {
			t.Fatal(err)
		}
		grants, _, err := c.Grants(ctx, territory, rs.SyncOpAttrs{PageToken: pagination.Token{Size: 100}})
		require.NoError(t, err)
		for _, gr := range grants {
			require.NotEqual(t, "0051X", gr.Principal.Id.Resource)
		}
	})

	t.Run("should handle idempotent revoke", func(t *testing.T) {
		principal := &v2.Resource{
			Id: &v2.ResourceId{ResourceType: resourceTypeUser.Id, Resource: "NONEXISTENT"},
		}

		g := &v2.Grant{
			Principal:   principal,
			Entitlement: ent,
		}

		annos, err := c.Revoke(ctx, g)
		require.NoError(t, err)
		test.AssertContainsAnnotation(t, &v2.GrantAlreadyRevoked{}, annos)
	})

	roleEnt := &v2.Entitlement{
		Id:       sdkEnt.NewEntitlementID(territory, territoryRolePermissionPrefix+"Owner"),
		Resource: territory,
		Slug:     territoryRolePermissionPrefix + "Owner",
	}

	t.Run("should grant role to non-member", func(t *testing.T) {
		if err := uhttp.ClearCaches(ctx); err != nil {
			t.Fatal(err)
		}
		// 0053X is not in T2
		principal := &v2.Resource{
			Id: &v2.ResourceId{ResourceType: resourceTypeUser.Id, Resource: "0053X"},
		}

		grants, annos, err := c.Grant(ctx, principal, roleEnt)
		require.NoError(t, err)
		test.AssertNoRatelimitAnnotations(t, annos)
		// Should return member grant + role:Owner grant
		require.Len(t, grants, 2)
		entIDs := make(map[string]bool)
		for _, g := range grants {
			entIDs[g.Entitlement.GetId()] = true
		}
		require.True(t, entIDs["territory:T2:member"])
		require.True(t, entIDs["territory:T2:role:Owner"])
	})

	t.Run("should grant role to existing member", func(t *testing.T) {
		if err := uhttp.ClearCaches(ctx); err != nil {
			t.Fatal(err)
		}
		// 0052X is in T2 (added in "should grant territory membership") with no role
		principal := &v2.Resource{
			Id: &v2.ResourceId{ResourceType: resourceTypeUser.Id, Resource: "0052X"},
		}

		grants, annos, err := c.Grant(ctx, principal, roleEnt)
		require.NoError(t, err)
		test.AssertNoRatelimitAnnotations(t, annos)
		// Should return only the role:Owner grant (user already a member)
		require.Len(t, grants, 1)
		require.Equal(t, "territory:T2:role:Owner", grants[0].Entitlement.GetId())
	})

	t.Run("should revoke territory role", func(t *testing.T) {
		if err := uhttp.ClearCaches(ctx); err != nil {
			t.Fatal(err)
		}
		// Revoke role:Owner from 0053X (added in "should grant role to non-member")
		principal := &v2.Resource{
			Id: &v2.ResourceId{ResourceType: resourceTypeUser.Id, Resource: "0053X"},
		}

		g := &v2.Grant{
			Principal:   principal,
			Entitlement: roleEnt,
		}

		annos, err := c.Revoke(ctx, g)
		require.NoError(t, err)
		test.AssertNoRatelimitAnnotations(t, annos)

		// 0053X should still be a member but without a role grant
		if err := uhttp.ClearCaches(ctx); err != nil {
			t.Fatal(err)
		}
		grants, _, err := c.Grants(ctx, territory, rs.SyncOpAttrs{PageToken: pagination.Token{Size: 100}})
		require.NoError(t, err)

		memberFound := false
		roleFound := false
		for _, gr := range grants {
			if gr.Principal.Id.Resource == "0053X" {
				if gr.Entitlement.GetId() == "territory:T2:member" {
					memberFound = true
				}
				if gr.Entitlement.GetId() == "territory:T2:role:Owner" {
					roleFound = true
				}
			}
		}
		require.True(t, memberFound, "0053X should still be a member of T2")
		require.False(t, roleFound, "0053X should not have a role grant in T2")
	})
}
