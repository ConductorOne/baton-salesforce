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

func TestPermissionSetGroupList(t *testing.T) {
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
	c := newPermissionSetGroupBuilder(salesforceClient)

	t.Run("should list permission set groups", func(t *testing.T) {
		resources := make([]*v2.Resource, 0)
		pToken := pagination.Token{Token: "", Size: 10}
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
		require.Len(t, resources, 1)
		require.Equal(t, "Test PSG", resources[0].DisplayName)
	})
}

func TestPermissionSetGroupGrants(t *testing.T) {
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
	c := newPermissionSetGroupBuilder(salesforceClient)

	t.Run("should return user grants for permission set group", func(t *testing.T) {
		psg, err := permissionSetGroupResource(&client.PermissionSetGroup{ID: "PSG1X", MasterLabel: "Test PSG"})
		require.Nil(t, err)

		grants := make([]*v2.Grant, 0)
		pToken := pagination.Token{Token: "", Size: 10}
		for {
			nextGrants, results, err := c.Grants(ctx, psg, rs.SyncOpAttrs{PageToken: pToken})
			grants = append(grants, nextGrants...)
			require.Nil(t, err)
			require.NotNil(t, results)
			test.AssertNoRatelimitAnnotations(t, results.Annotations)
			if results.NextPageToken == "" {
				break
			}
			pToken.Token = results.NextPageToken
		}
		require.Len(t, grants, 1)
		require.Equal(t, resourceTypeUser.Id, grants[0].Principal.Id.ResourceType)
		require.Equal(t, "0051X", grants[0].Principal.Id.Resource)
	})
}

func TestPermissionSetGroupGrantAndRevoke(t *testing.T) {
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
	c := newPermissionSetGroupBuilder(salesforceClient)

	psg, err := permissionSetGroupResource(&client.PermissionSetGroup{ID: "PSG1X", MasterLabel: "Test PSG"})
	require.Nil(t, err)

	psgEntitlement := v2.Entitlement{
		Id:       entitlement.NewEntitlementID(psg, permissionSetGroupMemberEntitlementName),
		Resource: psg,
	}

	t.Run("should grant user to permission set group", func(t *testing.T) {
		user, err := userResource(ctx, &client.SalesforceUser{ID: "0052X"}, nil, false)
		require.Nil(t, err)

		newGrants, grantAnnotations, err := c.Grant(ctx, user, &psgEntitlement)
		require.Nil(t, err)
		test.AssertNoRatelimitAnnotations(t, grantAnnotations)
		require.Len(t, newGrants, 1)

		if err := uhttp.ClearCaches(ctx); err != nil {
			t.Fatal(err)
		}

		grants := make([]*v2.Grant, 0)
		pToken := pagination.Token{Token: "", Size: 10}
		for {
			nextGrants, results, err := c.Grants(ctx, psg, rs.SyncOpAttrs{PageToken: pToken})
			grants = append(grants, nextGrants...)
			require.Nil(t, err)
			if results.NextPageToken == "" {
				break
			}
			pToken.Token = results.NextPageToken
		}
		require.Len(t, grants, 2)

		revokeAnnotations, err := c.Revoke(ctx, newGrants[0])
		require.Nil(t, err)
		test.AssertNoRatelimitAnnotations(t, revokeAnnotations)

		if err := uhttp.ClearCaches(ctx); err != nil {
			t.Fatal(err)
		}

		grantsAfter := make([]*v2.Grant, 0)
		pTokenAfter := pagination.Token{Token: "", Size: 10}
		for {
			nextGrants, results, err := c.Grants(ctx, psg, rs.SyncOpAttrs{PageToken: pTokenAfter})
			grantsAfter = append(grantsAfter, nextGrants...)
			require.Nil(t, err)
			if results.NextPageToken == "" {
				break
			}
			pTokenAfter.Token = results.NextPageToken
		}
		require.Len(t, grantsAfter, 1)
	})

	t.Run("should return GrantAlreadyExists for duplicate grant", func(t *testing.T) {
		user, err := userResource(ctx, &client.SalesforceUser{ID: "0051X"}, nil, false)
		require.Nil(t, err)

		_, grantAnnotations, err := c.Grant(ctx, user, &psgEntitlement)
		require.Nil(t, err)
		test.AssertContainsAnnotation(t, &v2.GrantAlreadyExists{}, grantAnnotations)
	})

	t.Run("should return GrantAlreadyRevoked when revoking non-existent membership", func(t *testing.T) {
		user, err := userResource(ctx, &client.SalesforceUser{ID: "0053X"}, nil, false)
		require.Nil(t, err)

		revokeGrant := &v2.Grant{
			Entitlement: &psgEntitlement,
			Principal:   user,
		}
		revokeAnnotations, err := c.Revoke(ctx, revokeGrant)
		require.Nil(t, err)
		test.AssertContainsAnnotation(t, &v2.GrantAlreadyRevoked{}, revokeAnnotations)
	})
}
