package client

import (
	"errors"
	"testing"

	"github.com/conductorone/simpleforce"
	"github.com/stretchr/testify/require"
)

// TestWrapOptionalSObjectErrorPassthrough pins the D3 value-identical
// passthrough contract: a non-INVALID_TYPE error is returned unchanged (same
// error value, no re-wrap), and nil in yields nil out.
func TestWrapOptionalSObjectErrorPassthrough(t *testing.T) {
	origErr := errors.New("boom")
	got := wrapOptionalSObjectError(origErr, "Territory2", "Enterprise Territory Management 2.0", "unblock instructions")
	require.Equal(t, origErr, got, "non-INVALID_TYPE error must be returned value-identically, not re-wrapped")
	// require.Same asserts the same underlying pointer, which is strictly
	// stronger than errors.Is (a re-wrapped error would pass ErrorIs but fail
	// Same) — this keeps the D3 value-identity pin intact.
	require.Same(t, origErr, got, "passthrough must return the identical error value")

	require.Nil(t, wrapOptionalSObjectError(nil, "Territory2", "Enterprise Territory Management 2.0", "unblock instructions"))
}

// TestWrapOptionalSObjectErrorInvalidType pins the wrap branch: an INVALID_TYPE
// SalesforceError is wrapped into an actionable error that keeps the vendor
// error reachable through errors.As (the %w chain) with its ErrorCode intact.
func TestWrapOptionalSObjectErrorInvalidType(t *testing.T) {
	sfErr := simpleforce.SalesforceError{
		Message:   "sObject type 'Territory2' is not supported.",
		ErrorCode: "INVALID_TYPE",
	}
	wrapped := wrapOptionalSObjectError(sfErr, "Territory2", "Enterprise Territory Management 2.0", "unblock instructions")
	require.Error(t, wrapped)
	require.Contains(t, wrapped.Error(), "Enterprise Territory Management 2.0")
	require.Contains(t, wrapped.Error(), "INVALID_TYPE")

	var gotErr simpleforce.SalesforceError
	require.True(t, errors.As(wrapped, &gotErr))
	require.Equal(t, "INVALID_TYPE", gotErr.ErrorCode)
	require.Contains(t, gotErr.Message, "sObject type 'Territory2' is not supported.")
}
