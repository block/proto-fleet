package hostedaccounts

import (
	"strconv"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A manager's request authorized before a demotion commits must not mutate
// invitations once it acquires its locks.
func TestTeamInvitationMutationsRecheckAuthorityAfterLocking(t *testing.T) {
	for _, mutation := range []string{"revoke", "delete"} {
		t.Run(mutation, func(t *testing.T) {
			svc, conn, _ := registrationFixture(t)
			teams := NewTeamInvitationService(svc.accounts, svc.sessions)
			var orgID, hostID int64
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT organization_id, inviter_id FROM hosted_account_invitation`).Scan(&orgID, &hostID))
			role := func(key string) int64 {
				var id int64
				require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT id FROM role WHERE organization_id=$1 AND builtin_key=$2`, orgID, key).Scan(&id))
				return id
			}
			q := sqlc.New(conn)
			managerID, err := q.CreateUser(t.Context(), sqlc.CreateUserParams{UserID: uuid.NewString(), Username: "queued-manager", PasswordHash: "fixture", CreatedAt: time.Now()})
			require.NoError(t, err)
			_, err = conn.ExecContext(t.Context(), `INSERT INTO user_organization (user_id, organization_id, role_id) VALUES ($1, $2, $3)`, managerID, orgID, role("ADMIN"))
			require.NoError(t, err)
			_, err = q.AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: managerID, OrganizationID: orgID, RoleID: role("ADMIN"), ScopeType: "org"})
			require.NoError(t, err)
			issued, err := teams.Create(t.Context(), hostID, orgID, strconv.FormatInt(role("FIELD_TECH"), 10), time.Hour)
			require.NoError(t, err)
			invitationID := issued.Invitation.InvitationID
			if mutation == "delete" {
				require.NoError(t, teams.Revoke(t.Context(), hostID, orgID, invitationID))
			}

			tx, err := conn.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			_, err = tx.ExecContext(t.Context(), `UPDATE user_organization_role SET deleted_at=now() WHERE user_id=$1 AND organization_id=$2 AND deleted_at IS NULL`, managerID, orgID)
			require.NoError(t, err)
			_, err = sqlc.New(tx).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: managerID, OrganizationID: orgID, RoleID: role("FIELD_TECH"), ScopeType: "org"})
			require.NoError(t, err)
			_, err = tx.ExecContext(t.Context(), `UPDATE user_organization SET role_id=$3 WHERE user_id=$1 AND organization_id=$2`, managerID, orgID, role("FIELD_TECH"))
			require.NoError(t, err)

			done := make(chan error, 1)
			go func() {
				if mutation == "revoke" {
					done <- teams.Revoke(t.Context(), managerID, orgID, invitationID)
				} else {
					done <- teams.Delete(t.Context(), managerID, orgID, invitationID)
				}
			}()
			require.Eventually(t, func() bool {
				var waiting int
				err := conn.QueryRowContext(t.Context(), `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'`).Scan(&waiting)
				return err == nil && waiting > 0 || len(done) > 0
			}, 5*time.Second, 10*time.Millisecond)
			require.Empty(t, done, "the mutation waits for the uncommitted demotion")
			require.NoError(t, tx.Commit())

			err = <-done
			require.True(t, fleeterror.IsForbiddenError(err), "a demoted manager's queued request is rejected: %v", err)
			var rows, revoked int
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*), count(revoked_at) FROM organization_invitation WHERE invitation_id=$1`, invitationID).Scan(&rows, &revoked))
			require.Equal(t, 1, rows, "the invitation is not deleted")
			if mutation == "revoke" {
				require.Zero(t, revoked, "the invitation stays pending")
			}
		})
	}
}
