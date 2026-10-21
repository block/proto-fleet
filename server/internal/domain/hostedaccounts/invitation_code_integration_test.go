package hostedaccounts

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
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
