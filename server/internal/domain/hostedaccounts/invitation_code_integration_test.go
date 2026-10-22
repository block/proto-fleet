package hostedaccounts

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestInvitationCodeCollisionsRetryWithoutReplacingExistingInvitation(t *testing.T) {
	svc, conn, token := registrationFixture(t)
	hash, err := invitationHash(token)
	require.NoError(t, err)
	original, err := sqlc.New(conn).GetHostedAccountInvitationByHash(t.Context(), hash)
	require.NoError(t, err)
	attempts := 0
	svc.generateCode = func() (string, error) {
		attempts++
		if attempts == 1 {
			return token, nil
		}
		return "K7MP-4X9R", nil
	}
	issued, err := svc.Create(t.Context(), original.InviterID, original.OrganizationID, original.ExternalAccountID, RoleViewer, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 2, attempts)
	require.Equal(t, "K7MP-4X9R", issued.Token)
	after, err := sqlc.New(conn).GetHostedAccountInvitationByHash(t.Context(), hash)
	require.NoError(t, err)
	require.Equal(t, original, after)
	svc.generateCode = func() (string, error) { return token, nil }
	_, err = svc.Create(t.Context(), original.InviterID, original.OrganizationID, original.ExternalAccountID, RoleViewer, time.Hour)
	require.Error(t, err, "persistent collisions fail instead of overwriting another invitation")
	var count int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_invitation`).Scan(&count))
	require.Equal(t, 2, count)
}

func TestInvitationCodesAreUniqueAcrossAccountAndTeamInvitations(t *testing.T) {
	svc, conn, accountCode := registrationFixture(t)
	hash, err := invitationHash(accountCode)
	require.NoError(t, err)
	original, err := sqlc.New(conn).GetHostedAccountInvitationByHash(t.Context(), hash)
	require.NoError(t, err)
	var fieldTech int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT id FROM role WHERE organization_id=$1 AND builtin_key='FIELD_TECH'`, original.OrganizationID).Scan(&fieldTech))
	teams := NewTeamInvitationService(svc.accounts, svc.sessions)
	attempts := 0
	teams.generateCode = func() (string, error) {
		attempts++
		if attempts == 1 {
			return accountCode, nil
		}
		return "K7MP-4X9R", nil
	}
	issued, err := teams.Create(t.Context(), original.InviterID, original.OrganizationID, strconv.FormatInt(fieldTech, 10), time.Hour)
	require.NoError(t, err)
	require.Equal(t, 2, attempts, "a customer invitation's code is never reissued as a team code")
	require.Equal(t, "K7MP-4X9R", issued.Token)
	owns, err := teams.Owns(t.Context(), accountCode)
	require.NoError(t, err)
	require.False(t, owns)
	svc.generateCode = func() (string, error) { return issued.Token, nil }
	_, err = svc.Create(t.Context(), original.InviterID, original.OrganizationID, original.ExternalAccountID, RoleViewer, time.Hour)
	require.Error(t, err, "nor is a team code reissued for a customer account")
}

// Issuers in different organizations hold different organization locks, so
// only the code reservation keeps them from committing the same code.
func TestInvitationCodeReservationSerializesIssuersAcrossOrganizations(t *testing.T) {
	svc, conn, _ := registrationFixture(t)
	q := sqlc.New(conn)
	var otherOrg int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO organization (org_id, name) VALUES ($1, 'Other team') RETURNING id`, uuid.NewString()).Scan(&otherOrg))
	require.NoError(t, authz.Reconcile(t.Context(), conn))
	var superAdmin, fieldTech int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT
		(SELECT id FROM role WHERE organization_id=$1 AND builtin_key='SUPER_ADMIN'),
		(SELECT id FROM role WHERE organization_id=$1 AND builtin_key='FIELD_TECH')`, otherOrg).Scan(&superAdmin, &fieldTech))
	otherHost, err := q.CreateUser(t.Context(), sqlc.CreateUserParams{UserID: uuid.NewString(), Username: "other-team-owner", PasswordHash: "fixture", CreatedAt: time.Now()})
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO user_organization (user_id, organization_id, role_id) VALUES ($1, $2, $3)`, otherHost, otherOrg, superAdmin)
	require.NoError(t, err)
	_, err = q.AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: otherHost, OrganizationID: otherOrg, RoleID: superAdmin, ScopeType: "org"})
	require.NoError(t, err)

	const contested = "VQ2W-8HNT"
	hash, err := invitationHash(contested)
	require.NoError(t, err)
	// A client account issuer in the first organization has reserved and
	// inserted the code but not yet committed.
	tx, err := conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	require.NoError(t, sqlc.New(tx).LockInvitationTokenHash(t.Context(), hash))
	_, err = tx.ExecContext(t.Context(), `
INSERT INTO hosted_account_invitation (invitation_id, organization_id, account_id, inviter_id, authority_fingerprint, token_hash, role, expires_at)
SELECT gen_random_uuid()::text, organization_id, account_id, inviter_id, authority_fingerprint, $1, role, expires_at
FROM hosted_account_invitation LIMIT 1`, hash)
	require.NoError(t, err)

	teams := NewTeamInvitationService(svc.accounts, svc.sessions)
	attempts := 0
	teams.generateCode = func() (string, error) {
		attempts++
		if attempts == 1 {
			return contested, nil
		}
		return "K7MP-4X9R", nil
	}
	type result struct {
		issued IssuedTeamInvitation
		err    error
	}
	done := make(chan result, 1)
	go func() {
		issued, err := teams.Create(t.Context(), otherHost, otherOrg, strconv.FormatInt(fieldTech, 10), time.Hour)
		done <- result{issued, err}
	}()
	require.Eventually(t, func() bool {
		var waiting int
		err := conn.QueryRowContext(t.Context(), `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory'`).Scan(&waiting)
		return err == nil && waiting > 0 || len(done) > 0
	}, 5*time.Second, 10*time.Millisecond)
	require.Empty(t, done, "the team issuer waits for the reserved code")
	require.NoError(t, tx.Commit())

	got := <-done
	require.NoError(t, got.err)
	require.Equal(t, "K7MP-4X9R", got.issued.Token, "the team issuer retries after seeing the committed code")
	require.Equal(t, 2, attempts)
	var hosted, team int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT
		(SELECT count(*) FROM hosted_account_invitation WHERE token_hash=$1),
		(SELECT count(*) FROM organization_invitation WHERE token_hash=$1)`, hash).Scan(&hosted, &team))
	require.Equal(t, 1, hosted)
	require.Zero(t, team, "one code never belongs to two invitations")
}

func TestInvitationCodeNormalizationAcrossPreviewAndRedemption(t *testing.T) {
	for _, register := range []bool{false, true} {
		t.Run(map[bool]string{false: "accept", true: "register"}[register], func(t *testing.T) {
			svc, conn, token := registrationFixture(t)
			code := strings.ToLower(strings.ReplaceAll(token, "-", " "))
			preview, err := svc.Preview(t.Context(), " "+code+" ")
			require.NoError(t, err)
			require.Equal(t, "Customer", preview.AccountName)
			if register {
				_, err = svc.Register(t.Context(), code, "normalized-code-user", "password123", "", "")
			} else {
				var orgID int64
				require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT organization_id FROM hosted_account_invitation`).Scan(&orgID))
				q := sqlc.New(conn)
				userID, createErr := q.CreateUser(t.Context(), sqlc.CreateUserParams{UserID: uuid.NewString(), Username: "existing-user", PasswordHash: "fixture", CreatedAt: time.Now()})
				require.NoError(t, createErr)
				require.NoError(t, q.AddHostedAccountUserAffiliation(t.Context(), sqlc.AddHostedAccountUserAffiliationParams{UserID: userID, OrganizationID: orgID}))
				_, err = svc.Accept(t.Context(), code, userID, orgID)
			}
			require.NoError(t, err)
			_, err = svc.Preview(t.Context(), token)
			require.Error(t, err, "the canonical form cannot replay a normalized acceptance")
			sum := sha256.Sum256([]byte(strings.ReplaceAll(token, "-", "")))
			var stored string
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT token_hash FROM hosted_account_invitation`).Scan(&stored))
			require.Equal(t, hex.EncodeToString(sum[:]), stored)
		})
	}
}
