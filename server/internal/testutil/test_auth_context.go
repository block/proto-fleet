package testutil

import (
	"context"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// MockAuthContextForTesting creates a context with session info for testing.
// This sets up the session-based authentication context expected by domain services.
func MockAuthContextForTesting(ctx context.Context, userID, orgID int64) context.Context {
	info := &session.Info{
		SessionID:      "test-session-id",
		UserID:         userID,
		OrganizationID: orgID,
	}
	return authn.SetInfo(ctx, info)
}

// MockAuthContextWithSessionID creates a context with a custom session ID for testing.
// Use this when testing session-specific behavior like stream deduplication.
func MockAuthContextWithSessionID(ctx context.Context, sessionID string, userID, orgID int64) context.Context {
	info := &session.Info{
		SessionID:      sessionID,
		UserID:         userID,
		OrganizationID: orgID,
	}
	return authn.SetInfo(ctx, info)
}
