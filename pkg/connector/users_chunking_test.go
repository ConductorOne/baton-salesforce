package connector

import (
	"context"
	"fmt"
	"testing"

	"github.com/conductorone/baton-salesforce/test"
	"github.com/stretchr/testify/require"
)

// TestGetUserLoginsByUserIDs_Chunking exercises the multi-chunk branch in
// GetUserLoginsByUserIDs by seeding more UserLogin rows than the client's
// IN-clause chunk size (250) and asserting every input Id round-trips
// through the returned map.
//
// The existing TestUsersList only has 3 users and so runs the outer loop
// exactly once — it never crosses a chunk boundary. This test protects
// against regressions if the chunk size is lowered or the loop is refactored.
func TestGetUserLoginsByUserIDs_Chunking(t *testing.T) {
	ctx := context.Background()

	server, db, err := test.FixturesServer(ctx)
	require.NoError(t, err)
	defer test.TearDownDB(ctx, db)
	defer server.Close()

	salesforceClient, err := test.Client(ctx, server.URL)
	require.NoError(t, err)

	// 600 seeded UserLogin rows → 3 chunks at the current 250-per-chunk size
	// (250 + 250 + 100). IsFrozen/IsPasswordLocked are left empty: ramsql
	// treats 'true'/'false' string literals as bool, which conflicts with
	// the TEXT column type. Empty string parses to false via getBoolField,
	// which is fine — this test only needs to verify chunking.
	const seeded = 600
	userIDs := make([]string, 0, seeded)
	for i := range seeded {
		id := fmt.Sprintf("005TEST%010d", i)
		userIDs = append(userIDs, id)
		loginID := fmt.Sprintf("006LOGIN%09d", i)
		insert := fmt.Sprintf( //nolint:gosec // test-only fixture seeding; values are loop-generated, not user input.
			"INSERT INTO UserLogin (Id, UserId, IsFrozen, IsPasswordLocked) VALUES ('%s', '%s', '', '')",
			loginID, id,
		)
		_, err := db.ExecContext(ctx, insert)
		require.NoError(t, err)
	}

	logins, _, err := salesforceClient.GetUserLoginsByUserIDs(ctx, userIDs)
	require.NoError(t, err)
	require.Len(t, logins, seeded, "every input UserId should have a matching UserLogin in the returned map")

	for _, id := range userIDs {
		login, ok := logins[id]
		require.True(t, ok, "missing UserLogin for %s", id)
		require.Equal(t, id, login.UserId)
	}
}
