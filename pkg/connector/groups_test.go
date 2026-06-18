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

func TestGroupsList(t *testing.T) {
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
	c := newGroupBuilder(salesforceClient)

	t.Run("should get groups with pagination", func(t *testing.T) {
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
		group, _ := groupResource(&client.SalesforceGroup{ID: "00G1X"})
		user, _ := userResource(ctx, &client.SalesforceUser{ID: "0052X"}, nil, false)

		entitlement := v2.Entitlement{
			Id:       entitlement.NewEntitlementID(group, groupMemberEntitlementName),
			Resource: group,
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
			nextGrants, results, err := c.Grants(ctx, group, rs.SyncOpAttrs{PageToken: pToken})
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
		grantsAfter, results, err := c.Grants(ctx, group, rs.SyncOpAttrs{PageToken: pagination.Token{}})
		require.Nil(t, err)
		require.NotNil(t, results)
		test.AssertNoRatelimitAnnotations(t, results.Annotations)
		require.Equal(t, "", results.NextPageToken)
		require.Len(t, grantsAfter, 1)
	})

	t.Run("should allow double grant and double revoke", func(t *testing.T) {
		group, _ := groupResource(&client.SalesforceGroup{ID: "00G1X"})
		user, _ := userResource(ctx, &client.SalesforceUser{ID: "0052X"}, nil, false)

		entitlement := v2.Entitlement{
			Id:       entitlement.NewEntitlementID(group, groupMemberEntitlementName),
			Resource: group,
		}

		grantAnnotationsBefore, err := c.Grant(ctx, user, &entitlement)
		require.Nil(t, err)
		test.AssertNoRatelimitAnnotations(t, grantAnnotationsBefore)

		grantAnnotationsAfter, err := c.Grant(ctx, user, &entitlement)
		require.Nil(t, err)
		test.AssertNoRatelimitAnnotations(t, grantAnnotationsAfter)

		// TODO(marcos): We don't actually detect double grants.
		// test.AssertContainsAnnotation(t, &v2.GrantAlreadyExists{}, grantAnnotationsAfter)

		grant := v2.Grant{
			Entitlement: &entitlement,
			Principal:   user,
		}

		// Initial revoke is the same as any other revoke.
		revokeAnnotationsBefore, err := c.Revoke(ctx, &grant)
		require.Nil(t, err)
		test.AssertNoRatelimitAnnotations(t, revokeAnnotationsBefore)

		// Second revoke.
		if err := uhttp.ClearCaches(ctx); err != nil {
			t.Fatal(err)
		}
		revokeAnnotationsAfter, err := c.Revoke(ctx, &grant)
		require.Nil(t, err)
		test.AssertNoRatelimitAnnotations(t, revokeAnnotationsAfter)

		test.AssertContainsAnnotation(t, &v2.GrantAlreadyRevoked{}, revokeAnnotationsAfter)
	})
}
