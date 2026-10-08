package hostedaccounts_test

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestInvitationPaginationRetainsLifecycleHistoryAndTraversesTies(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.host, hostedaccounts.DefaultCapabilities)
	svc := invitations(f)
	active, err := svc.Create(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.RoleViewer, time.Hour)
	require.NoError(t, err)
	accepted, err := svc.Create(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.RoleViewer, time.Hour)
	require.NoError(t, err)
	_, err = svc.Accept(t.Context(), accepted.Token, f.user, f.org)
	require.NoError(t, err)
	revoked, err := svc.Create(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.RoleViewer, time.Hour)
	require.NoError(t, err)
	require.NoError(t, svc.Revoke(t.Context(), f.host, f.org, account.AccountID, revoked.Invitation.InvitationID))
	expired, err := svc.Create(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.RoleViewer, time.Hour)
	require.NoError(t, err)
	// Make ties wider than a page and include multiple timestamps. PostgreSQL
	// stores microsecond precision, so use a whole second for exact assertions.
	createdAt := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Hour)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_invitation SET created_at=$1 WHERE organization_id=$2 AND account_id=$3`, createdAt, f.org, account.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_invitation SET expires_at=$1 WHERE invitation_id=$2`, createdAt.Add(time.Hour), expired.Invitation.InvitationID)
	require.NoError(t, err)
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	for i := range 103 {
		id := uuid.NewString()
		hash := sha256.Sum256([]byte(id))
		timestamp := createdAt
		if i >= 70 {
			timestamp = createdAt.Add(-time.Hour)
		}
		_, err = tx.ExecContext(t.Context(), `INSERT INTO hosted_account_invitation
			(invitation_id,organization_id,account_id,inviter_id,authority_fingerprint,token_hash,role,created_at,expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,'viewer',$7,$8)`, id, f.org, account.ID, f.host, strings.Repeat("a", 64), fmt.Sprintf("%x", hash), timestamp, createdAt.Add(24*time.Hour))
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())

	first, next, err := svc.List(t.Context(), f.host, f.org, account.AccountID, 0, "")
	require.NoError(t, err)
	require.Len(t, first, 25, "zero requests the bounded default")
	require.NotEmpty(t, next)
	maxPage, maxNext, err := svc.List(t.Context(), f.host, f.org, account.AccountID, 100, "")
	require.NoError(t, err)
	require.Len(t, maxPage, 100)
	require.NotEmpty(t, maxNext)

	var all []hostedaccounts.Invitation
	var token string
	for pages := 0; ; pages++ {
		require.Less(t, pages, 20, "pagination must terminate")
		page, next, err := svc.List(t.Context(), f.host, f.org, account.AccountID, 13, token)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page), 13)
		all = append(all, page...)
		if next == "" {
			break
		}
		token = next
	}
	require.Len(t, all, 107)
	seen := make(map[string]hostedaccounts.Invitation)
	for i, row := range all {
		_, duplicate := seen[row.InvitationID]
		require.False(t, duplicate, "an invitation appears on only one page")
		seen[row.InvitationID] = row
		if i > 0 {
			previous := all[i-1]
			require.True(t, previous.CreatedAt.After(row.CreatedAt) || (previous.CreatedAt.Equal(row.CreatedAt) && previous.InvitationID > row.InvitationID), "pages preserve the database's total ordering")
		}
	}
	require.False(t, seen[active.Invitation.InvitationID].AcceptedAt.Valid)
	require.False(t, seen[active.Invitation.InvitationID].RevokedAt.Valid)
	require.True(t, seen[accepted.Invitation.InvitationID].AcceptedAt.Valid)
	require.True(t, seen[revoked.Invitation.InvitationID].RevokedAt.Valid)
	require.True(t, seen[expired.Invitation.InvitationID].ExpiresAt.Before(time.Now()))
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM hosted_account_invitation WHERE organization_id=$1 AND account_id=$2`, f.org, account.ID).Scan(&count))
	require.Equal(t, len(all), count, "paging preserves all historical records")
}

func TestInvitationPaginationRejectsInvalidAndCrossScopeTokens(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.host, hostedaccounts.DefaultCapabilities)
	other := f.client(t, "Other", f.host, hostedaccounts.DefaultCapabilities)
	svc := invitations(f)
	for range 3 {
		_, err := svc.Create(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.RoleViewer, time.Hour)
		require.NoError(t, err)
	}
	_, token, err := svc.List(t.Context(), f.host, f.org, account.AccountID, 1, "")
	require.NoError(t, err)
	require.NotEmpty(t, token)
	_, _, err = svc.List(t.Context(), f.host, f.org, other.AccountID, 1, token)
	require.True(t, fleeterror.IsInvalidArgumentError(err))
	var otherOrg int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO organization (org_id, name) VALUES ($1, 'Other host') RETURNING id`, uuid.NewString()).Scan(&otherOrg))
	_, _, err = svc.List(t.Context(), f.host, otherOrg, account.AccountID, 1, token)
	require.True(t, fleeterror.IsInvalidArgumentError(err), "a token is bound to its organization before loading accounts")
	for _, invalid := range []string{"not base64!", strings.Repeat("a", 513), base64.RawURLEncoding.EncodeToString([]byte("1|invalid")), token + "="} {
		_, _, err = svc.List(t.Context(), f.host, f.org, account.AccountID, 1, invalid)
		require.True(t, fleeterror.IsInvalidArgumentError(err), "invalid token: %q", invalid)
	}
	for _, invalid := range []int32{-1, 101} {
		_, _, err = svc.List(t.Context(), f.host, f.org, account.AccountID, invalid, "")
		require.True(t, fleeterror.IsInvalidArgumentError(err))
	}
}

func TestInvitationPaginationRechecksMembershipBetweenPages(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, account.AccountID, f.user, hostedaccounts.RoleAdministrator))
	svc := invitations(f)
	for range 2 {
		_, err := svc.Create(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.RoleViewer, time.Hour)
		require.NoError(t, err)
	}
	page, next, err := svc.List(t.Context(), f.user, f.org, account.AccountID, 1, "")
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.NotEmpty(t, next)
	require.NoError(t, f.svc.RevokeMembership(t.Context(), f.host, f.org, account.AccountID, f.user))
	_, _, err = svc.List(t.Context(), f.user, f.org, account.AccountID, 1, next)
	require.True(t, fleeterror.IsForbiddenError(err), "continuation tokens cannot preserve revoked access")
}
