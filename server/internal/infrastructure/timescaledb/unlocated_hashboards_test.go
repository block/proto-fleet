package timescaledb

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestUnlocatedHashboardsDoNotCreatePlacements(t *testing.T) {
	for _, entirely := range []bool{false, true} {
		t.Run(map[bool]string{false: "mixed", true: "entirely"}[entirely], func(t *testing.T) {
			r := newContainerRollupRig(t)
			s := r.snapshot(0, false, 100e12, 200e12)
			s.TopologyComplete = true // Unlocated inventory must override this claim.
			s.UnlocatedHashboards = []sdk.ContainerHashboard{{Occupancy: "occupied", Serial: "inventory-only", ObservedAt: r.base, HashrateHS: new(50e12)}}
			want, placements := 350e12, 2
			if entirely {
				s.Tanks = nil
				want = 50e12
				placements = 0
			}
			r.store(t, s)
			require.False(t, s.TopologyComplete)
			require.Equal(t, want, *s.Metrics.HashrateHS)
			var hash float64
			var count int
			require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT hashrate_hs FROM container_latest_metrics WHERE device_id=$1", r.databaseID).Scan(&hash))
			require.Equal(t, want, hash)
			require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard_installation").Scan(&count))
			require.Equal(t, placements, count)
			require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard_identity WHERE value='inventory-only'").Scan(&count))
			require.Equal(t, 1, count, "identity is retained without inventing an installation")
			for _, tank := range s.Tanks {
				require.Positive(t, tank.Position)
			}
			if !entirely {
				require.Equal(t, 100e12, *s.Tanks[0].Metrics.HashrateHS)
				require.Equal(t, 200e12, *s.Tanks[1].Metrics.HashrateHS)
			}
		})
	}
}

func TestUnlocatedAliasClaimsPreventFalsePlacement(t *testing.T) {
	for _, serialOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "unlocated MAC", true: "unlocated serial"}[serialOnly], func(t *testing.T) {
			r := newContainerRollupRig(t)
			first := r.snapshot(0, false, 100e12, 200e12)
			first.Tanks[0].Modules[0].Hashboards[0].MACAddress = "aa:bb:cc:dd:ee:01"
			r.store(t, first)
			next := r.snapshot(time.Second, false, 100e12, 200e12)
			next.TopologyComplete = false
			next.Tanks[0].Modules[0].Hashboards[0] = sdk.UnavailableHashboard(1, "")
			next.UnlocatedHashboards = []sdk.ContainerHashboard{{Occupancy: "occupied", MACAddress: "AA-BB-CC-DD-EE-01", ObservedAt: next.ObservedAt, HashrateHS: new(100e12)}}
			mapped := &next.Tanks[1].Modules[0].Hashboards[0]
			mapped.Serial = "board-a"
			if serialOnly {
				next.UnlocatedHashboards[0].MACAddress = ""
				next.UnlocatedHashboards[0].Serial = "board-a"
				mapped.MACAddress = "aa:bb:cc:dd:ee:01"
				mapped.Serial = ""
			}
			r.store(t, next)
			require.Equal(t, sdk.HashboardIdentityConflict, next.UnlocatedHashboards[0].UnavailableReason)
			require.Equal(t, sdk.HashboardIdentityConflict, next.Tanks[1].Modules[0].Hashboards[0].UnavailableReason)
			require.Nil(t, next.Metrics.HashrateHS)
			var tank, count int
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT t.position FROM hashboard_installation i JOIN hashboard_identity a ON a.hashboard_id=i.hashboard_id JOIN container_module m ON m.id=i.module_id JOIN container_tank t ON t.id=m.tank_id WHERE a.kind='serial' AND a.value='board-a' AND i.removed_at IS NULL`).Scan(&tank))
			require.Equal(t, 1, tank)
			require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard_installation WHERE removed_at IS NOT NULL").Scan(&count))
			require.Zero(t, count)
		})
	}
}

func TestUnlocatedReadingsRetainSourceOrdering(t *testing.T) {
	r := newContainerRollupRig(t)
	makePoll := func(poll, source time.Duration, located bool) *sdk.ContainerSnapshot {
		s := r.snapshot(poll, false, 100e12, 200e12)
		s.TopologyComplete = false
		board := s.Tanks[0].Modules[0].Hashboards[0]
		board.ObservedAt = r.base.Add(source)
		if located {
			s.Tanks[0].Modules[0].Hashboards[0] = board
		} else {
			board.Position = 0
			s.UnlocatedHashboards = []sdk.ContainerHashboard{board}
			s.Tanks[0].Modules[0].Hashboards[0] = sdk.UnavailableHashboard(1, "")
		}
		return s
	}
	r.store(t, makePoll(0, 0, false))
	for _, located := range []bool{false, true} {
		s := makePoll(time.Second, -time.Second, located)
		if located {
			s.ObservedAt = r.base.Add(2 * time.Second)
		}
		r.store(t, s)
		require.Equal(t, 200e12, *s.Metrics.HashrateHS, "an older sample cannot reenter via an unlocated or mapped observation")
	}
	recovered := makePoll(3*time.Second, 3*time.Second, true)
	r.store(t, recovered)
	require.Equal(t, 300e12, *recovered.Metrics.HashrateHS)
	var hash sql.NullFloat64
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT hashrate_hs FROM container_latest_metrics WHERE device_id=$1", r.databaseID).Scan(&hash))
	require.True(t, hash.Valid)
	require.Equal(t, 300e12, hash.Float64)
}

func unlocatedControllerRigs(t *testing.T) (*containerRollupRig, *containerRollupRig) {
	t.Helper()
	dbSvc := testutil.NewDatabaseService(t, nil)
	user := dbSvc.CreateSuperAdminUser()
	a, b := dbSvc.CreateDevice(user.OrganizationID, "proto"), dbSvc.CreateDevice(user.OrganizationID, "proto")
	_, err := dbSvc.DB.ExecContext(t.Context(), "UPDATE discovered_device SET device_class=2 WHERE id IN (SELECT discovered_device_id FROM device WHERE id IN ($1,$2))", a.DatabaseID, b.DatabaseID)
	require.NoError(t, err)
	telemetry, err := NewTelemetryStore(dbSvc.DB, DefaultConfig())
	require.NoError(t, err)
	base := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Microsecond)
	return &containerRollupRig{db: dbSvc.DB, telemetry: telemetry, deviceID: a.ID, databaseID: a.DatabaseID, base: base}, &containerRollupRig{db: dbSvc.DB, telemetry: telemetry, deviceID: b.ID, databaseID: b.DatabaseID, base: base}
}

func singleInventoryBoard(r *containerRollupRig, poll, source time.Duration, mapped bool) *sdk.ContainerSnapshot {
	s := r.snapshot(poll, false, 100e12, 0)
	s.TopologyComplete = false
	s.Tanks = s.Tanks[:1]
	b := s.Tanks[0].Modules[0].Hashboards[0]
	b.ObservedAt = r.base.Add(source)
	b.MACAddress = "aa:bb:cc:dd:ee:01"
	if mapped {
		s.Tanks[0].Modules[0].Hashboards[0] = b
	} else {
		b.Position = 0
		s.Tanks = nil
		s.UnlocatedHashboards = []sdk.ContainerHashboard{b}
	}
	sdk.CalculateContainerRollups(s)
	return s
}

func TestUnlocatedAlternatingControllers(t *testing.T) {
	a, b := unlocatedControllerRigs(t)
	for i := range 6 {
		offset := time.Duration(i) * time.Second
		s := singleInventoryBoard(a, offset, offset, false)
		if i%2 == 0 {
			a.store(t, s)
			require.Equal(t, 100e12, *s.Metrics.HashrateHS)
		} else {
			// A different alias for the same durable board must still conflict.
			s.UnlocatedHashboards[0].MACAddress = ""
			b.store(t, s)
			require.Equal(t, sdk.HashboardControllerConflict, s.UnlocatedHashboards[0].UnavailableReason)
			require.Nil(t, s.Metrics.HashrateHS)
		}
	}
	// Rejected claims must not extend ownership; transfer after A's last poll expires.
	transferred := singleInventoryBoard(b, 124*time.Second, 124*time.Second, false)
	b.store(t, transferred)
	require.Equal(t, 100e12, *transferred.Metrics.HashrateHS)
	lateA := singleInventoryBoard(a, 125*time.Second, 125*time.Second, true)
	a.store(t, lateA)
	require.Nil(t, lateA.Metrics.HashrateHS)
}

func TestUnlocatedSourceOrderingAcrossControllers(t *testing.T) {
	for _, mapped := range []bool{false, true} {
		t.Run(map[bool]string{false: "unlocated", true: "first placement"}[mapped], func(t *testing.T) {
			a, b := unlocatedControllerRigs(t)
			a.store(t, singleInventoryBoard(a, 0, 4*time.Second, false))
			// Ownership has expired, but the old reading remains within the freshness window.
			older := singleInventoryBoard(b, 120*time.Second, 3*time.Second, mapped)
			if mapped {
				board := &older.Tanks[0].Modules[0].Hashboards[0]
				board.StateObservedAt = a.base.Add(3 * time.Second)
				board.HealthJSON = json.RawMessage(`{"fault":"prior controller diagnostic"}`)
				board.HashrailOK = new(false)
				board.RefclksOK = new(false)
			}
			b.store(t, older)
			if mapped {
				var count int
				require.NoError(t, b.db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard_metrics").Scan(&count))
				require.Zero(t, count, "first placement must not persist cached health/state from the previous controller")
			}
			require.Nil(t, older.Metrics.HashrateHS)
			var retained time.Time
			require.NoError(t, b.db.QueryRowContext(t.Context(), "SELECT source_at FROM hashboard_observation").Scan(&retained))
			require.Equal(t, a.base.Add(4*time.Second), retained.UTC(), "an allowed transfer cannot lower the durable source watermark")
			// The attribution boundary must survive subsequent polls too.
			repeated := singleInventoryBoard(b, 121*time.Second, 4*time.Second, mapped)
			b.store(t, repeated)
			require.Nil(t, repeated.Metrics.HashrateHS)
			fresh := singleInventoryBoard(b, 122*time.Second, 122*time.Second, mapped)
			b.store(t, fresh)
			require.Equal(t, 100e12, *fresh.Metrics.HashrateHS)
			regressed := singleInventoryBoard(b, 123*time.Second, 121*time.Second, mapped)
			b.store(t, regressed)
			require.Nil(t, regressed.Metrics.HashrateHS, "source ordering is enforced independently of the transfer boundary")
		})
	}
}

func TestUnlocatedSiteAttributionBoundary(t *testing.T) {
	for _, initiallyMapped := range []bool{false, true} {
		t.Run(map[bool]string{false: "never installed", true: "previously mapped"}[initiallyMapped], func(t *testing.T) {
			r := newContainerRollupRig(t)
			var org, siteA, siteB int64
			require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT org_id FROM device WHERE id=$1", r.databaseID).Scan(&org))
			require.NoError(t, r.db.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES ($1,'A','a') RETURNING id", org).Scan(&siteA))
			require.NoError(t, r.db.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES ($1,'B','b') RETURNING id", org).Scan(&siteB))
			var latestPoll time.Time
			for i, site := range []*int64{nil, &siteA, &siteB, nil} {
				_, err := r.db.ExecContext(t.Context(), "UPDATE device SET site_id=$1 WHERE id=$2", site, r.databaseID)
				require.NoError(t, err)
				pollAt := time.Now().UTC().Add(20 * time.Millisecond).Truncate(time.Microsecond)
				if !latestPoll.IsZero() && !pollAt.After(latestPoll) {
					pollAt = latestPoll.Add(10 * time.Millisecond)
				}
				offset := pollAt.Sub(r.base)
				if i > 0 {
					for _, poll := range []time.Duration{offset, offset + 10*time.Millisecond} {
						cached := singleInventoryBoard(r, poll, offset-time.Second, false)
						r.store(t, cached)
						require.Nil(t, cached.Metrics.HashrateHS, "prior-site sample must not regain attribution on a later poll")
					}
				}
				freshAt := offset + 20*time.Millisecond
				fresh := singleInventoryBoard(r, freshAt, freshAt, i == 0 && initiallyMapped)
				r.store(t, fresh)
				require.Equal(t, 100e12, *fresh.Metrics.HashrateHS)
				latestPoll = r.base.Add(freshAt)
			}
		})
	}
}

func TestHashboardObservationMigrationPreservesExistingAttribution(t *testing.T) {
	for _, released := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "removed"}[released], func(t *testing.T) {
			r := newContainerRollupRig(t)
			r.store(t, r.snapshot(0, false, 100e12, 200e12))
			r.store(t, r.snapshot(time.Second, true, 110e12, 210e12))
			if released {
				empty := r.snapshot(2*time.Second, true, 0, 0)
				for ti := range empty.Tanks {
					empty.Tanks[ti].Modules[0].Hashboards[0] = sdk.ContainerHashboard{Position: 1, Occupancy: "empty"}
				}
				r.store(t, empty)
			}
			down, err := migrations.Current.ReadFile("current/001023_internal_hashboard_observation.down.sql")
			require.NoError(t, err)
			up, err := migrations.Current.ReadFile("current/001023_internal_hashboard_observation.up.sql")
			require.NoError(t, err)
			for range 2 {
				_, err = r.db.ExecContext(t.Context(), string(down))
				require.NoError(t, err)
				_, err = r.db.ExecContext(t.Context(), string(up))
				require.NoError(t, err)
				var count int
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hashboard_observation WHERE device_id=$1 AND attribution_since=$2 AND source_at=$2 AND (released_at IS NOT NULL)=$3`, r.databaseID, r.base.Add(time.Second), released).Scan(&count))
				require.Equal(t, 2, count)
				var seen time.Time
				require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT min(last_seen_at) FROM hashboard_observation").Scan(&seen))
				want := r.base.Add(time.Second)
				if released {
					want = r.base.Add(2 * time.Second)
				}
				require.Equal(t, want, seen.UTC())
			}
		})
	}
}

func TestUnlocatedObservationKeepsOwnershipWhenSlotIsCleared(t *testing.T) {
	a, b := unlocatedControllerRigs(t)
	a.store(t, singleInventoryBoard(a, 0, 0, true))
	next := singleInventoryBoard(a, time.Second, time.Second, true)
	board := next.Tanks[0].Modules[0].Hashboards[0]
	board.Position = 0
	next.UnlocatedHashboards = []sdk.ContainerHashboard{board}
	next.Tanks[0].Modules[0].Hashboards[0] = sdk.ContainerHashboard{Position: 1, Occupancy: "empty"}
	a.store(t, next)
	require.Equal(t, 100e12, *next.Metrics.HashrateHS)
	conflicting := singleInventoryBoard(b, 2*time.Second, 2*time.Second, false)
	b.store(t, conflicting)
	require.Equal(t, sdk.HashboardControllerConflict, conflicting.UnlocatedHashboards[0].UnavailableReason)
	require.Nil(t, conflicting.Metrics.HashrateHS)
}
