package timescaledb

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestHistoryPaginationRejectsTierChanges(t *testing.T) {
	for _, entity := range []string{"hashboard", "module"} {
		for _, tiers := range [][2]int{{0, 3600}, {3600, 86400}} {
			t.Run(fmt.Sprintf("%s_%d_to_%d", entity, tiers[0], tiers[1]), func(t *testing.T) {
				r := newContainerRollupRig(t)
				r.base = time.Now().UTC().Truncate(24 * time.Hour).Add(-40 * 24 * time.Hour)
				r.store(t, r.snapshot(time.Hour+10*time.Second, false, 100e12, 200e12))
				offset := time.Hour + 20*time.Second
				if tiers[0] > 0 {
					offset = 2*time.Hour + 10*time.Second
				}
				r.store(t, r.snapshot(offset, false, 300e12, 200e12))
				// An intersecting summary bucket may begin before the requested start.
				start, end := r.base.Add(time.Hour+5*time.Second), r.base.Add(3*time.Hour)
				refreshStart, refreshEnd := r.base, r.base.Add(24*time.Hour)
				for _, table := range []string{"hashboard", "container_module"} {
					for _, tier := range []string{"hourly", "daily"} {
						testutil.RefreshContinuousAggregate(t, r.db, table+"_metrics_"+tier, &refreshStart, &refreshEnd)
					}
				}
				var board, org, module int64
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT hashboard_id,org_id FROM hashboard_identity WHERE kind='serial' AND value='board-a'`).Scan(&board, &org))
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT module_id FROM hashboard_installation WHERE hashboard_id=$1`, board).Scan(&module))
				// Change only this disposable database's routing function to simulate aging
				// across a boundary deterministically, without wall-clock sleeps.
				setTier := func(tier int) {
					_, err := r.db.ExecContext(t.Context(), fmt.Sprintf(`CREATE OR REPLACE FUNCTION container_history_resolution(start_time timestamptz,end_time timestamptz) RETURNS integer LANGUAGE sql STABLE AS $$ SELECT %d $$`, tier))
					require.NoError(t, err)
				}
				reader := fleetinventory.NewService(sqlc.New(r.db))
				scopes := []fleetinventory.Scope{{OrgWide: true}}
				read := func(token string) (int, string, error) {
					if entity == "hashboard" {
						response, err := reader.HashboardHistory(t.Context(), org, &pb.ListHashboardHistoryRequest{HashboardId: board, StartTime: timestamppb.New(start), EndTime: timestamppb.New(end), PageSize: 1, PageToken: token}, scopes)
						if err != nil {
							return 0, "", err
						}
						return len(response.Samples), response.NextPageToken, nil
					}
					response, err := reader.History(t.Context(), org, &pb.ListModuleHistoryRequest{ModuleId: module, StartTime: timestamppb.New(start), EndTime: timestamppb.New(end), PageSize: 1, PageToken: token}, scopes)
					if err != nil {
						return 0, "", err
					}
					return len(response.Samples), response.NextPageToken, nil
				}
				setTier(tiers[0])
				count, token, err := read("")
				require.NoError(t, err)
				require.Equal(t, 1, count)
				require.NotEmpty(t, token)
				count, next, err := read(token)
				require.NoError(t, err)
				require.Equal(t, 1, count)
				require.Empty(t, next)
				// Tokens issued before resolution was recorded cannot safely continue.
				decoded, err := base64.RawURLEncoding.DecodeString(token)
				require.NoError(t, err)
				var legacy map[string]any
				require.NoError(t, json.Unmarshal(decoded, &legacy))
				delete(legacy, "resolution_seconds")
				encoded, err := json.Marshal(legacy)
				require.NoError(t, err)
				_, _, err = read(base64.RawURLEncoding.EncodeToString(encoded))
				require.True(t, fleeterror.IsFailedPreconditionError(err), "legacy cursor must require restart: %v", err)
				setTier(tiers[1])
				// Previously this returned an empty final page despite unread history.
				_, _, err = read(token)
				require.True(t, fleeterror.IsFailedPreconditionError(err), "changed tier must require restart: %v", err)
				require.ErrorContains(t, err, "restart pagination")
				count, _, err = read("")
				require.NoError(t, err)
				require.Equal(t, 1, count, "restart must find retained summary")
			})
		}
	}
}

// Keep real SQL access while moving the boundary between resolution selection
// and row retrieval. The data query must use the resolution already selected.
type historyTierSwitchStore struct {
	fleetinventory.Store
	afterResolution func()
}

func (s *historyTierSwitchStore) GetHistoryResolution(ctx context.Context, params sqlc.GetHistoryResolutionParams) (int32, error) {
	resolution, err := s.Store.GetHistoryResolution(ctx, params)
	if err == nil {
		s.afterResolution()
	}
	return resolution, err
}

func TestHistoryPaginationKeepsTierWithinRequest(t *testing.T) {
	for _, entity := range []string{"hashboard", "module"} {
		t.Run(entity, func(t *testing.T) {
			r := newContainerRollupRig(t)
			r.base = time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
			r.store(t, r.snapshot(10*time.Second, false, 100e12, 200e12))
			r.store(t, r.snapshot(20*time.Second, false, 300e12, 200e12))
			start, end := r.base, r.base.Add(time.Hour)
			for _, table := range []string{"hashboard_metrics_hourly", "container_module_metrics_hourly"} {
				testutil.RefreshContinuousAggregate(t, r.db, table, &start, &end)
			}
			var board, module, org int64
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT hashboard_id,org_id FROM hashboard_identity WHERE kind='serial' AND value='board-a'`).Scan(&board, &org))
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT module_id FROM hashboard_installation WHERE hashboard_id=$1`, board).Scan(&module))
			store := &historyTierSwitchStore{Store: sqlc.New(r.db), afterResolution: func() {
				_, err := r.db.ExecContext(t.Context(), `CREATE OR REPLACE FUNCTION container_history_resolution(start_time timestamptz,end_time timestamptz) RETURNS integer LANGUAGE sql STABLE AS $$ SELECT 3600 $$`)
				require.NoError(t, err)
			}}
			reader := fleetinventory.NewService(store)
			scopes := []fleetinventory.Scope{{OrgWide: true}}
			var token string
			if entity == "hashboard" {
				response, err := reader.HashboardHistory(t.Context(), org, &pb.ListHashboardHistoryRequest{HashboardId: board, StartTime: timestamppb.New(start), EndTime: timestamppb.New(end), PageSize: 1}, scopes)
				require.NoError(t, err)
				require.Len(t, response.Samples, 1)
				require.Zero(t, response.Samples[0].ResolutionSeconds)
				token = response.NextPageToken
			} else {
				response, err := reader.History(t.Context(), org, &pb.ListModuleHistoryRequest{ModuleId: module, StartTime: timestamppb.New(start), EndTime: timestamppb.New(end), PageSize: 1}, scopes)
				require.NoError(t, err)
				require.Len(t, response.Samples, 1)
				require.True(t, response.Samples[0].ObservedAt.AsTime().Equal(r.base.Add(10*time.Second)))
				token = response.NextPageToken
			}
			require.NotEmpty(t, token, "two raw samples must not collapse into one hourly summary mid-request")
		})
	}
}
