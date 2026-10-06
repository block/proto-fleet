package containers_test

import (
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	inventorypb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	telemetry "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/timescaledb"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestModuleIssuesInventoryLifecycle(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	org, err := sqlc.New(db).CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "module-issues", Name: "Module issues"})
	require.NoError(t, err)
	d := &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{DeviceIdentifier: "issues-ct1", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, Model: "CT1", DriverName: "proto", IpAddress: "192.0.2.10", Port: "8080", UrlScheme: "http"}, OrgID: org, IsActive: true}
	_, err = sqlstores.NewSQLDiscoveredDeviceStore(db).Save(ctx, discoverymodels.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: d.DeviceIdentifier}, d)
	require.NoError(t, err)
	devices := sqlstores.NewSQLDeviceStore(db)
	require.NoError(t, devices.InsertDevice(ctx, &d.Device, org, d.DeviceIdentifier))
	require.NoError(t, devices.UpsertDevicePairing(ctx, &d.Device, org, "PAIRED"))
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	evidence := func(at time.Time, causes ...string) *sdk.ContainerModuleIssues {
		out := &sdk.ContainerModuleIssues{ObservedAt: at, Complete: true, SourceTimes: map[string]time.Time{"hashboard:left": at, "hashboard:right": at, "psu": at}}
		for _, cause := range causes {
			out.Items = append(out.Items, sdk.ContainerModuleIssue{Component: "psu", ComponentID: "psu", Cause: cause, Severity: "fault", ObservedAt: at})
		}
		return out
	}
	snapshot := &sdk.ContainerSnapshot{ObservedAt: base, TopologyComplete: true, Tanks: []sdk.ContainerTank{{Position: 1, Modules: []sdk.ContainerModule{{Position: 1, ReportedID: "original", State: sdk.ModuleAttention, ObservedAt: base, Issues: evidence(base, "PSU_OVER_TEMPERATURE_FAULT")}, {Position: 2, ReportedID: "other", State: sdk.ModuleHashing, ObservedAt: base, Issues: evidence(base)}}}}}
	module := &snapshot.Tanks[0].Modules[0]
	write := func() {
		t.Helper()
		require.NoError(t, store.StoreDeviceMetrics(ctx, telemetry.DeviceMetrics{DeviceIdentifier: d.DeviceIdentifier, Timestamp: snapshot.ObservedAt, Container: snapshot}))
	}
	reader := fleetinventory.NewService(sqlc.New(db))
	read := func() *inventorypb.InventoryItem {
		t.Helper()
		result, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_MINING_ASSETS}, []fleetinventory.Scope{{OrgWide: true}})
		require.NoError(t, err)
		require.Len(t, result.Items, 2)
		var first *inventorypb.InventoryItem
		for _, item := range result.Items {
			if item.Position == 1 {
				first = item
			} else {
				require.Empty(t, item.ModuleIssues.Items, "another module must not inherit the cause")
			}
		}
		require.NotNil(t, first)
		return first
	}
	write()
	first := read()
	originalID := first.Ref.Id
	require.Len(t, first.ModuleIssues.Items, 1)
	require.Equal(t, "psu", first.ModuleIssues.Items[0].Component)
	// Invalid source times must not poison ordering watermarks or clear current issues.
	for _, test := range []struct {
		name       string
		sourceTime time.Time
		observedAt time.Time
	}{
		{"future source", base.Add(24 * time.Hour), base},
		{"zero source", time.Time{}, base},
		{"future source without aggregate time", base.Add(24 * time.Hour), time.Time{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot.ObservedAt = base.Add(500 * time.Millisecond)
			module.Issues = evidence(test.observedAt)
			module.Issues.SourceTimes["hashboard:right"] = test.sourceTime
			err := store.StoreDeviceMetrics(ctx, telemetry.DeviceMetrics{DeviceIdentifier: d.DeviceIdentifier, Timestamp: snapshot.ObservedAt, Container: snapshot})
			require.ErrorContains(t, err, "invalid module issue source time")
			require.Len(t, read().ModuleIssues.Items, 1)
		})
	}
	// A newer collection of older complete evidence cannot clear the fault.
	snapshot.ObservedAt = base.Add(time.Second)
	module.Issues = evidence(base.Add(-time.Second))
	write()
	require.Len(t, read().ModuleIssues.Items, 1)
	// Explicit resolution replaces the current set.
	snapshot.ObservedAt = base.Add(2 * time.Second)
	module.Issues = evidence(snapshot.ObservedAt)
	write()
	require.Empty(t, read().ModuleIssues.Items)
	require.True(t, read().ModuleIssues.Complete)
	// Missing evidence is unknown, and does not reset source ordering.
	snapshot.ObservedAt = base.Add(3 * time.Second)
	module.Issues = nil
	write()
	require.Nil(t, read().ModuleIssues)
	snapshot.ObservedAt = base.Add(4 * time.Second)
	module.Issues = evidence(base, "OLD_FAULT")
	write()
	require.Nil(t, read().ModuleIssues)
	// New evidence restores availability.
	snapshot.ObservedAt = base.Add(5 * time.Second)
	module.Issues = evidence(snapshot.ObservedAt, "CURRENT_FAULT")
	write()
	require.Len(t, read().ModuleIssues.Items, 1)
	_, err = db.ExecContext(ctx, "UPDATE container_module SET last_seen_at=now()-INTERVAL '3 minutes' WHERE position=1")
	require.NoError(t, err)
	stale := read().ModuleIssues
	require.True(t, stale.Stale)
	require.False(t, stale.Complete)
	require.Empty(t, stale.Items)
	// Replacement gets its own issue set, and delayed topology cannot resurrect the old identity.
	snapshot.ObservedAt = base.Add(6 * time.Second)
	module.ReportedID = "replacement"
	module.Issues = evidence(snapshot.ObservedAt)
	write()
	replacement := read()
	require.NotEqual(t, originalID, replacement.Ref.Id)
	require.Empty(t, replacement.ModuleIssues.Items)
	snapshot.ObservedAt = base
	module.ReportedID = "original"
	module.Issues = evidence(base, "OLD_FAULT")
	write()
	require.Equal(t, replacement.Ref.Id, read().Ref.Id)
	require.Empty(t, read().ModuleIssues.Items)
	// Independently sampled boards: the oldest time can tie while another board resolves.
	snapshot.ObservedAt = base.Add(7 * time.Second)
	module.ReportedID = "replacement"
	module.Issues = evidence(snapshot.ObservedAt, "BOARD_FAULT")
	write()
	snapshot.ObservedAt = base.Add(8 * time.Second)
	module.Issues = evidence(base.Add(7 * time.Second))
	module.Issues.SourceTimes["hashboard:right"] = snapshot.ObservedAt
	write()
	require.Empty(t, read().ModuleIssues.Items, "a newer board can resolve even when the oldest source ties")
	snapshot.ObservedAt = base.Add(9 * time.Second)
	module.Issues = evidence(base.Add(7*time.Second), "OLD_BOARD_FAULT")
	write()
	require.Empty(t, read().ModuleIssues.Items, "an older individual board cannot resurrect its fault")
	// Rejecting a regressed board must not advance another board's watermark.
	snapshot.ObservedAt = base.Add(10 * time.Second)
	module.Issues = evidence(base.Add(6*time.Second), "NEW_BOARD_FAULT")
	module.Issues.SourceTimes["hashboard:right"] = snapshot.ObservedAt
	write()
	require.Empty(t, read().ModuleIssues.Items)
	snapshot.ObservedAt = base.Add(11 * time.Second)
	module.Issues = evidence(base.Add(7*time.Second), "NEW_BOARD_FAULT")
	module.Issues.SourceTimes["hashboard:right"] = base.Add(10 * time.Second)
	write()
	require.Len(t, read().ModuleIssues.Items, 1, "correcting the regressed board must admit the same new fault timestamp")
	// Rejected partial evidence withdraws confirmation without consuming newer sources.
	snapshot.ObservedAt = base.Add(12 * time.Second)
	module.Issues = evidence(base.Add(6*time.Second), "NEXT_BOARD_FAULT")
	module.Issues.Complete = false
	module.Issues.SourceTimes["hashboard:right"] = snapshot.ObservedAt
	write()
	require.Nil(t, read().ModuleIssues)
	snapshot.ObservedAt = base.Add(13 * time.Second)
	module.Issues = evidence(base.Add(7*time.Second), "NEXT_BOARD_FAULT")
	module.Issues.SourceTimes["hashboard:right"] = base.Add(12 * time.Second)
	write()
	require.Len(t, read().ModuleIssues.Items, 1)
	// Repeated partial evidence is idempotent and retains explicit faults.
	snapshot.ObservedAt = base.Add(14 * time.Second)
	module.Issues = evidence(snapshot.ObservedAt, "PARTIAL_FAULT")
	module.Issues.Complete = false
	write()
	require.Len(t, read().ModuleIssues.Items, 1)
	require.False(t, read().ModuleIssues.Complete)
	snapshot.ObservedAt = base.Add(15 * time.Second)
	write()
	repeated := read().ModuleIssues
	require.NotNil(t, repeated, "repeating identical partial evidence must retain its fault")
	require.Len(t, repeated.Items, 1)
	require.False(t, repeated.Complete)
	// Site-scoped inventory cannot expose the controller's child issue evidence.
	result, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_MINING_ASSETS}, []fleetinventory.Scope{{Sites: []int64{999}}})
	require.NoError(t, err)
	require.Empty(t, result.Items)
}
