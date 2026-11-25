package mininggroups

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDeactivationWaitsForBlockingFrigateUpdate(t *testing.T) {
	for _, tc := range []struct {
		state     string
		retryable bool
		blocking  bool
	}{
		{state: "staging", blocking: true},
		{state: "staged", blocking: true},
		{state: "starting", blocking: true},
		{state: "running", blocking: true},
		{state: "failed", retryable: true, blocking: true},
		{state: "failed"},
		{state: "succeeded"},
		{state: "cancelled"},
		{state: "verified"},
		{state: "abandoned"},
	} {
		name := tc.state
		if tc.retryable {
			name += " retryable"
		}
		t.Run(name, func(t *testing.T) {
			ctx, f := deactivationFixture(t)
			info, err := session.GetInfo(ctx)
			require.NoError(t, err)
			q := sqlc.New(f.conn)
			pkg, err := q.InsertFrigateUpdatePackage(ctx, sqlc.InsertFrigateUpdatePackageParams{ID: uuid.New(), OrgID: f.target.OrganizationID, Kind: "frigate", Filename: "frigate.deb", Version: "2.0.0", Architecture: "arm64", Sha256: strings.Repeat("a", 64), SizeBytes: 1, Repository: "btc-mining/frigate", ReleaseKeyID: "key", IssuedAt: time.Now().UTC(), Statement: []byte(`{}`), Signature: []byte{1}, UploadedBy: sql.NullInt64{Int64: info.UserID, Valid: true}})
			require.NoError(t, err)
			job, err := q.InsertFrigateUpdateJob(ctx, sqlc.InsertFrigateUpdateJobParams{ID: uuid.New(), OrgID: f.target.OrganizationID, DeviceID: f.target.DeviceID, PackageID: pkg.ID, TargetVersion: pkg.Version, ExpectedInstanceIds: []string{"controller"}, RequestKey: uuid.NewString(), ActorUserID: info.UserID, ActorExternalID: "operator", ActorUsername: "operator"})
			require.NoError(t, err)
			_, err = f.conn.ExecContext(ctx, `UPDATE frigate_update_job SET state=$2,retryable=$3 WHERE id=$1`, job.ID, tc.state, tc.retryable)
			require.NoError(t, err)
			_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, uuid.NewString())
			if tc.blocking {
				require.ErrorContains(t, err, "controller software update")
				_, err = f.conn.ExecContext(ctx, `UPDATE frigate_update_job SET state='cancelled',retryable=false WHERE id=$1`, job.ID)
				require.NoError(t, err)
				_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, uuid.NewString())
			}
			require.NoError(t, err, "terminal update history must not block deactivation")
		})
	}
}
