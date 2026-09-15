package command

import (
	"database/sql"
	"strings"
	"testing"

	"connectrpc.com/authn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

func TestFirmwareCommandActivitySeparatesSystemActorFromBatchOwner(t *testing.T) {
	for _, caller := range []string{"enforcement", "human", "API key"} {
		t.Run(caller, func(t *testing.T) {
			svc, conn, identifier, _ := newTransactionalCommandFixture(t)
			activityStore := sqlstores.NewSQLActivityStore(conn)
			svc.activitySvc = activity.NewService(activityStore)
			var completed onFinishedCallbackFunc
			svc.startStatusUpdateRoutineOverride = func(_ string, callback onFinishedCallbackFunc) { completed = callback }
			info := &session.Info{
				UserID: 42, OrganizationID: 1, ExternalUserID: "user-1", Username: "test-user",
				AuthMethod: session.AuthMethodSession, SessionID: "session-42",
			}
			identifiers := []string{identifier}
			if caller == "enforcement" {
				info.AuthMethod = ""
				info.Actor = session.ActorRolloutEnforcement
				// An internal actor must never create a synthetic human in the
				// activity log, even if a caller supplies these session fields.
				info.ExternalUserID, info.Username = "rollout-enforcement", "rollout-enforcement"
				identifiers = append(identifiers, "skipped-miner")
				svc.filters = []CommandFilter{newFakeFilter("test filter", "skipped-miner")}
			} else if caller == "API key" {
				info.AuthMethod, info.SessionID, info.APIKeyID = session.AuthMethodAPIKey, "", "deployment-key"
			}
			ctx := authn.SetInfo(t.Context(), info)
			fileID, err := svc.filesService.SaveFirmwareFile("assigned.swu", strings.NewReader("assigned firmware"), files.FirmwareMetadata{
				TargetManufacturer: "TestCorp", TargetModel: "TestMiner", FirmwareVersion: "2.0.0",
			})
			require.NoError(t, err)
			artifact, err := svc.filesService.ResolveFirmwareArtifact(fileID)
			require.NoError(t, err)
			result, err := svc.FirmwareUpdateArtifact(ctx, includeSelector(identifiers...), artifact.Checksum, artifact.Metadata)
			require.NoError(t, err)
			require.NotNil(t, completed)
			var batchOwner int64
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT created_by FROM command_batch_log WHERE uuid = $1`, result.BatchIdentifier).Scan(&batchOwner))
			assert.Equal(t, int64(42), batchOwner, "the persisted assignment owner remains the command batch's user owner")
			_, err = conn.ExecContext(ctx, `INSERT INTO command_on_device_log (command_batch_log_id, device_id, status, org_id)
				SELECT batch.id, message.device_id, 'SUCCESS', batch.organization_id FROM command_batch_log batch
				JOIN queue_message message ON message.command_batch_log_uuid = batch.uuid WHERE batch.uuid = $1`, result.BatchIdentifier)
			require.NoError(t, err)
			require.NoError(t, completed())

			rows, err := conn.QueryContext(ctx, `SELECT event_type, actor_type, user_id, username
				FROM activity_log WHERE organization_id = 1 ORDER BY id`)
			require.NoError(t, err)
			defer rows.Close()
			var eventTypes []string
			for rows.Next() {
				var eventType, actorType string
				var userID, username sql.NullString
				require.NoError(t, rows.Scan(&eventType, &actorType, &userID, &username))
				eventTypes = append(eventTypes, eventType)
				if caller == "enforcement" {
					assert.Equal(t, string(activitymodels.ActorSystem), actorType, eventType)
					assert.False(t, userID.Valid, eventType)
					assert.False(t, username.Valid, eventType)
				} else {
					assert.Equal(t, string(activitymodels.ActorUser), actorType, eventType)
					assert.Equal(t, sql.NullString{String: "user-1", Valid: true}, userID, eventType)
					assert.Equal(t, sql.NullString{String: "test-user", Valid: true}, username, eventType)
				}
			}
			require.NoError(t, rows.Err())
			expectedTypes := []string{"firmware_update", "firmware_update.completed"}
			if caller == "enforcement" {
				expectedTypes = append(expectedTypes, "command_filter_skip")
			}
			assert.ElementsMatch(t, expectedTypes, eventTypes)
			users, err := activityStore.GetDistinctUsers(ctx, 1)
			require.NoError(t, err)
			if caller == "enforcement" {
				assert.Empty(t, users, "background commands must not add a synthetic human to the activity user filter")
			} else {
				assert.Equal(t, []activitymodels.UserInfo{{UserID: "user-1", Username: "test-user"}}, users)
			}
		})
	}
}
