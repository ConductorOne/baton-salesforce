package connector

import (
	"context"
	"testing"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	"github.com/conductorone/baton-salesforce/test"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/stretchr/testify/require"
)

// These tests deliberately never call uhttp.ClearCaches between steps: the
// lookups that Grant and Revoke use to decide whether to write must not be
// answered from the HTTP response cache with a result that a previous write
// made outdated.

func setupProvisioningCacheTest(t *testing.T) (context.Context, *client.SalesforceClient) {
	ctx := context.Background()

	server, db, err := test.FixturesServer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		test.TearDownDB(ctx, db)
		server.Close()
	})

	salesforceClient, err := test.Client(ctx, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	// Start clean so entries cached by earlier tests can't leak in.
	if err := uhttp.ClearCaches(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, salesforceClient
}

func hasGrantAlreadyRevoked(t *testing.T, annos annotations.Annotations) bool {
	var alreadyRevoked v2.GrantAlreadyRevoked
	found, err := test.UnmarshalFromAnys(&alreadyRevoked, annos)
	require.NoError(t, err)
	return found
}

func hasGrantAlreadyExists(t *testing.T, annos annotations.Annotations) bool {
	var alreadyExists v2.GrantAlreadyExists
	found, err := test.UnmarshalFromAnys(&alreadyExists, annos)
	require.NoError(t, err)
	return found
}

func TestPermissionSetRevokeAfterGrantNotCached(t *testing.T) {
	ctx, salesforceClient := setupProvisioningCacheTest(t)
	c := newPermissionBuilder(salesforceClient)

	permission, _ := permissionResource(&client.SalesforcePermission{ID: "345X"})
	ent := &v2.Entitlement{
		Id:       entitlement.NewEntitlementID(permission, permissionSetAssignmentEntitlementName),
		Resource: permission,
		Slug:     permissionSetAssignmentEntitlementName,
	}
	// 0052X is not assigned to permission set 345X.
	principal := &v2.Resource{Id: &v2.ResourceId{ResourceType: resourceTypeUser.Id, Resource: "0052X"}}
	grant := &v2.Grant{Entitlement: ent, Principal: principal}

	annos, err := c.Revoke(ctx, grant)
	require.NoError(t, err)
	require.True(t, hasGrantAlreadyRevoked(t, annos), "revoke of a missing assignment")

	annos, err = c.Grant(ctx, principal, ent)
	require.NoError(t, err)
	require.False(t, hasGrantAlreadyExists(t, annos))

	annos, err = c.Revoke(ctx, grant)
	require.NoError(t, err)
	require.False(t, hasGrantAlreadyRevoked(t, annos), "revoke must delete the assignment just granted")

	annos, err = c.Revoke(ctx, grant)
	require.NoError(t, err)
	require.True(t, hasGrantAlreadyRevoked(t, annos), "assignment should be gone")
}

func TestGroupRevokeAfterGrantNotCached(t *testing.T) {
	ctx, salesforceClient := setupProvisioningCacheTest(t)
	c := newGroupBuilder(salesforceClient)

	group, _ := groupResource(&client.SalesforceGroup{ID: "00G1X"})
	ent := &v2.Entitlement{
		Id:       entitlement.NewEntitlementID(group, groupMemberEntitlementName),
		Resource: group,
	}
	// 0052X is not a member of group 00G1X.
	principal, _ := userResource(ctx, &client.SalesforceUser{ID: "0052X"}, nil, false)
	grant := &v2.Grant{Entitlement: ent, Principal: principal}

	annos, err := c.Revoke(ctx, grant)
	require.NoError(t, err)
	require.True(t, hasGrantAlreadyRevoked(t, annos), "revoke of a missing membership")

	_, err = c.Grant(ctx, principal, ent)
	require.NoError(t, err)

	annos, err = c.Revoke(ctx, grant)
	require.NoError(t, err)
	require.False(t, hasGrantAlreadyRevoked(t, annos), "revoke must delete the membership just granted")

	annos, err = c.Revoke(ctx, grant)
	require.NoError(t, err)
	require.True(t, hasGrantAlreadyRevoked(t, annos), "membership should be gone")
}

func TestPermissionSetGroupGrantRevokeNotCached(t *testing.T) {
	ctx, salesforceClient := setupProvisioningCacheTest(t)
	c := newPermissionSetGroupBuilder(salesforceClient)

	psg, err := permissionSetGroupResource(&client.PermissionSetGroup{ID: "PSG1X", MasterLabel: "Test PSG"})
	require.NoError(t, err)
	ent := &v2.Entitlement{
		Id:       entitlement.NewEntitlementID(psg, permissionSetGroupMemberEntitlementName),
		Resource: psg,
	}
	// 0052X is not assigned to PSG1X.
	principal, _ := userResource(ctx, &client.SalesforceUser{ID: "0052X"}, nil, false)
	grant := &v2.Grant{Entitlement: ent, Principal: principal}

	// Grant looks the assignment up first and finds none.
	_, annos, err := c.Grant(ctx, principal, ent)
	require.NoError(t, err)
	require.False(t, hasGrantAlreadyExists(t, annos))

	annos, err = c.Revoke(ctx, grant)
	require.NoError(t, err)
	require.False(t, hasGrantAlreadyRevoked(t, annos), "revoke must delete the assignment just granted")

	// Revoke looked the assignment up and found it; Grant must not reuse that.
	_, annos, err = c.Grant(ctx, principal, ent)
	require.NoError(t, err)
	require.False(t, hasGrantAlreadyExists(t, annos), "grant must create the assignment just revoked")
}

func TestTerritoryRevokeAfterGrantNotCached(t *testing.T) {
	ctx, salesforceClient := setupProvisioningCacheTest(t)
	c := newTerritoryBuilder(salesforceClient)

	territory := &v2.Resource{
		Id:          &v2.ResourceId{ResourceType: resourceTypeTerritory.Id, Resource: "T2"},
		DisplayName: "Brasil",
	}
	ent := &v2.Entitlement{
		Id:       entitlement.NewEntitlementID(territory, territoryMemberPermission),
		Resource: territory,
		Slug:     territoryMemberPermission,
	}
	// 0052X is not a member of territory T2.
	principal := &v2.Resource{Id: &v2.ResourceId{ResourceType: resourceTypeUser.Id, Resource: "0052X"}}
	grant := &v2.Grant{Entitlement: ent, Principal: principal}

	annos, err := c.Revoke(ctx, grant)
	require.NoError(t, err)
	require.True(t, hasGrantAlreadyRevoked(t, annos), "revoke of a missing membership")

	_, annos, err = c.Grant(ctx, principal, ent)
	require.NoError(t, err)
	require.False(t, hasGrantAlreadyExists(t, annos), "grant must create the membership")

	annos, err = c.Revoke(ctx, grant)
	require.NoError(t, err)
	require.False(t, hasGrantAlreadyRevoked(t, annos), "revoke must delete the membership just granted")

	annos, err = c.Revoke(ctx, grant)
	require.NoError(t, err)
	require.True(t, hasGrantAlreadyRevoked(t, annos), "membership should be gone")
}

func TestRoleRevokeAfterGrantNotCached(t *testing.T) {
	ctx, salesforceClient := setupProvisioningCacheTest(t)
	c := newRoleBuilder(salesforceClient)

	role, _ := roleResource(&client.SalesforceRole{ID: "199X"})
	ent := &v2.Entitlement{
		Id:       entitlement.NewEntitlementID(role, roleAssignmentEntitlementName),
		Resource: role,
	}
	// 0052X has no role.
	principal, _ := userResource(ctx, &client.SalesforceUser{ID: "0052X"}, nil, false)

	_, err := c.Grant(ctx, principal, ent)
	require.NoError(t, err)

	// Revoke checks the user's current UserRoleId; it must not read the
	// user as cached by Grant, before the role was set.
	_, err = c.Revoke(ctx, &v2.Grant{Entitlement: ent, Principal: principal})
	require.NoError(t, err)
}
