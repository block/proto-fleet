package fleetinventory_test

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkerrors "github.com/block/proto-fleet/server/sdk/v1/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type diagnosticHistoryFixture struct {
	db            *sql.DB
	service       *fleetinventory.Service
	orgID         int64
	moduleID      int64
	boardID       int64
	installationA int64
	installationB int64
	siteA         int64
	siteB         int64
	sameAt        time.Time
	diagnosticAt  time.Time
	oldAt         time.Time
	siteBAt       time.Time
	issueAt       time.Time
}

func newDiagnosticHistoryFixture(t *testing.T) *diagnosticHistoryFixture {
	t.Helper()
	ctx := t.Context()
	database := testutil.NewDatabaseService(t, nil)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	db := database.DB

	var siteA, siteB int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO site(org_id,name,slug) VALUES ($1,'Diagnostic A','diagnostic-a') RETURNING id`, user.OrganizationID).Scan(&siteA))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO site(org_id,name,slug) VALUES ($1,'Diagnostic B','diagnostic-b') RETURNING id`, user.OrganizationID).Scan(&siteB))
	_, err := db.ExecContext(ctx, `UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, device.DatabaseID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE device SET site_id=$1 WHERE id=$2`, siteA, device.DatabaseID)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	sameAt := now.Add(-30 * time.Minute)
	diagnosticAt := sameAt.Add(time.Minute)
	oldAt := now.Add(-48 * time.Hour)
	siteBAt := sameAt.Add(10 * time.Minute)
	issueAt := diagnosticAt.Add(-5 * time.Second)

	_, err = db.ExecContext(ctx, `INSERT INTO container_inventory(device_id,observed_at,org_id) VALUES ($1,$2,$3)`, device.DatabaseID, siteBAt, user.OrganizationID)
	require.NoError(t, err)
	var tankID, moduleID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES ($1,1,$2,$3) RETURNING id`, device.DatabaseID, oldAt.Add(-time.Hour), siteBAt).Scan(&tankID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,reported_id,first_seen_at,last_seen_at,state,state_observed_at,observation_site_id) VALUES ($1,1,'module-a',$2,$3,'active',$3,$4) RETURNING id`, tankID, oldAt.Add(-time.Hour), siteBAt, siteA).Scan(&moduleID))

	var boardID, installationA, installationB int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES ($1,$2,$3) RETURNING id`, user.OrganizationID, oldAt.Add(-time.Hour), siteBAt).Scan(&boardID))
	_, err = db.ExecContext(ctx, `INSERT INTO hashboard_identity(org_id,kind,value,hashboard_id) VALUES ($1,'mac','aa:bb:cc:dd:ee:01',$2)`, user.OrganizationID, boardID)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,site_id,installed_at,last_seen_at,metadata) VALUES ($1,$2,1,$3,$4,$5,'{"macAddress":"aa:bb:cc:dd:ee:01","serial":"board-a"}') RETURNING id`, boardID, moduleID, siteA, oldAt.Add(-time.Hour), siteBAt.Add(-time.Minute)).Scan(&installationA))

	evidence := sdk.ContainerModuleIssues{
		ObservedAt: issueAt,
		Complete:   true,
		SourceTimes: map[string]time.Time{
			"hashboard:aa:bb:cc:dd:ee:01": issueAt,
		},
		Items: []sdk.ContainerModuleIssue{{
			Component:      "hashboard",
			ComponentID:    "aa:bb:cc:dd:ee:01",
			Cause:          "HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE",
			Severity:       "fault",
			CanonicalError: sdkerrors.HashboardOverTemperature,
			ObservedAt:     issueAt,
		}},
	}
	moduleIssues, err := json.Marshal(evidence)
	require.NoError(t, err)
	boardIssues, err := json.Marshal(evidence.Items)
	require.NoError(t, err)

	insertModuleDiagnostic := func(at time.Time, site int64, state string, stateAt time.Time, issues any) {
		t.Helper()
		_, err := db.ExecContext(ctx, `INSERT INTO container_module_diagnostic_observation(module_id,collected_at,site_id,attribution_since,state,state_observed_at,issues) VALUES ($1,$2,$3,$4,$5,$6,$7)`, moduleID, at, site, oldAt.Add(-time.Hour), state, stateAt, issues)
		require.NoError(t, err)
	}
	insertBoardDiagnostic := func(at time.Time, installation int64, state string, stateAt, healthAt time.Time, issues any) {
		t.Helper()
		_, err := db.ExecContext(ctx, `INSERT INTO hashboard_diagnostic_observation(hashboard_id,installation_id,collected_at,observed_at,state,state_observed_at,mining_state,isolated,hashrail_ok,refclks_ok,health,issues) VALUES ($1,$2,$3,$4,$5,$6,'MINING_STATE_ERROR',true,false,true,'{"errors":[{"cause":"over_temperature"}]}',$7)`, boardID, installation, at, healthAt, state, stateAt, issues)
		require.NoError(t, err)
	}

	// A numeric row and a diagnostic observation can share both ordering times.
	var topologyVersionID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO container_topology_version(device_id,fingerprint,topology) VALUES ($1,'numeric-history','{}') RETURNING id`, device.DatabaseID).Scan(&topologyVersionID))
	coverage := `{"hashrate":{"reporting":1,"expected":1,"topology_complete":true},"power":{"reporting":1,"expected":1,"topology_complete":true},"temperature":{"reporting":1,"expected":1,"topology_complete":true}}`
	_, err = db.ExecContext(ctx, `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,temperature_c,efficiency_jh,coverage,site_id,topology_version_id) VALUES ($1,$2,$2,100e12,3000,70,30e-12,$3,$4,$5)`, moduleID, sameAt, coverage, siteA, topologyVersionID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO hashboard_metrics(hashboard_id,installation_id,time,collected_at,state,state_observed_at,mining_state,isolated,hashrate_hs,temperature_c,power_w,efficiency_jh,hashrail_ok,refclks_ok,health,topology_version_id) VALUES ($1,$2,$3,$3,'active',$3,'MINING_STATE_ACTIVE',false,100e12,70,3000,30e-12,true,true,'{}',$4)`, boardID, installationA, sameAt, topologyVersionID)
	require.NoError(t, err)
	insertModuleDiagnostic(sameAt, siteA, "error", sameAt.Add(-time.Second), moduleIssues)
	insertBoardDiagnostic(sameAt, installationA, "error", sameAt.Add(-time.Second), sameAt.Add(-2*time.Second), boardIssues)

	// These observations have no numeric partner and must stay out of chart history.
	insertModuleDiagnostic(diagnosticAt, siteA, "error", issueAt, moduleIssues)
	insertBoardDiagnostic(diagnosticAt, installationA, "error", issueAt, issueAt, boardIssues)
	insertModuleDiagnostic(oldAt, siteA, "inactive", oldAt.Add(-time.Second), nil)
	insertBoardDiagnostic(oldAt, installationA, "inactive", oldAt.Add(-time.Second), oldAt.Add(-2*time.Second), []byte(`[]`))

	// Move the physical board to another historical installation. Its rows remain
	// authorized by installation site, independently of the current controller site.
	_, err = db.ExecContext(ctx, `UPDATE hashboard_installation SET removed_at=$1 WHERE id=$2`, siteBAt.Add(-time.Minute), installationA)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,site_id,installed_at,last_seen_at,metadata) VALUES ($1,$2,1,$3,$4,$5,'{"macAddress":"aa:bb:cc:dd:ee:01","serial":"board-a"}') RETURNING id`, boardID, moduleID, siteB, siteBAt.Add(-time.Minute), siteBAt).Scan(&installationB))
	insertBoardDiagnostic(siteBAt, installationB, "active", siteBAt, siteBAt, []byte(`[]`))
	insertModuleDiagnostic(siteBAt, siteB, "active", siteBAt, nil)

	return &diagnosticHistoryFixture{
		db: db, service: fleetinventory.NewService(sqlc.New(db)), orgID: user.OrganizationID,
		moduleID: moduleID, boardID: boardID, installationA: installationA, installationB: installationB,
		siteA: siteA, siteB: siteB, sameAt: sameAt, diagnosticAt: diagnosticAt, oldAt: oldAt,
		siteBAt: siteBAt, issueAt: issueAt,
	}
}

// Numeric history must remain bounded by metric samples, regardless of the
// number of diagnostic polls retained for backend processing.
func TestNumericHistoryExcludesDiagnostics(t *testing.T) {
	f := newDiagnosticHistoryFixture(t)
	ctx := t.Context()
	scopes := []fleetinventory.Scope{{OrgWide: true}}
	read := func(start, end time.Time, size int32) (*pb.ListModuleHistoryResponse, *pb.ListHashboardHistoryResponse) {
		t.Helper()
		modules, err := f.service.History(ctx, f.orgID, &pb.ListModuleHistoryRequest{ModuleId: f.moduleID, StartTime: timestamppb.New(start), EndTime: timestamppb.New(end), PageSize: size}, scopes)
		require.NoError(t, err)
		boards, err := f.service.HashboardHistory(ctx, f.orgID, &pb.ListHashboardHistoryRequest{HashboardId: f.boardID, StartTime: timestamppb.New(start), EndTime: timestamppb.New(end), PageSize: size}, scopes)
		require.NoError(t, err)
		return modules, boards
	}
	t.Run("diagnostic-only polls do not produce chart samples", func(t *testing.T) {
		modules, boards := read(f.diagnosticAt.Add(-time.Second), f.diagnosticAt.Add(time.Second), 1)
		require.Empty(t, modules.Samples)
		require.Empty(t, boards.Samples)
		require.Empty(t, modules.NextPageToken)
		require.Empty(t, boards.NextPageToken)
	})
	t.Run("matching diagnostics do not add pages to raw metrics", func(t *testing.T) {
		modules, boards := read(f.sameAt.Add(-time.Second), f.sameAt.Add(time.Second), 1)
		require.Len(t, modules.Samples, 1)
		require.Len(t, boards.Samples, 1)
		require.Equal(t, 100e12, modules.Samples[0].GetHashrateHs())
		require.Equal(t, 100e12, boards.Samples[0].GetHashrateHs())
		require.Empty(t, modules.NextPageToken)
		require.Empty(t, boards.NextPageToken)
	})
	t.Run("seven days of diagnostics do not exhaust numeric pagination", func(t *testing.T) {
		end := f.sameAt.Add(time.Hour).Truncate(time.Hour)
		start := end.Add(-7 * 24 * time.Hour)
		for _, view := range []string{"container_module_metrics_hourly", "hashboard_metrics_hourly"} {
			testutil.RefreshContinuousAggregate(t, f.db, view, &start, &end)
		}
		// A normal twenty-second poll produces 30,240 observations per week,
		// exceeding the client's twenty pages of one thousand rows.
		_, err := f.db.ExecContext(ctx, `INSERT INTO container_module_diagnostic_observation(module_id,collected_at,site_id,state)
   SELECT $1,at,$2,'active' FROM generate_series($3::timestamptz,$4::timestamptz-interval '20 seconds',interval '20 seconds') at ON CONFLICT DO NOTHING`, f.moduleID, f.siteA, start, end)
		require.NoError(t, err)
		_, err = f.db.ExecContext(ctx, `INSERT INTO hashboard_diagnostic_observation(hashboard_id,installation_id,collected_at,state,mining_state)
   SELECT $1,$2,at,'active','MINING_STATE_ACTIVE' FROM generate_series($3::timestamptz,$4::timestamptz-interval '20 seconds',interval '20 seconds') at ON CONFLICT DO NOTHING`, f.boardID, f.installationA, start, end)
		require.NoError(t, err)
		modules, boards := read(start, end, 1000)
		require.Len(t, modules.Samples, 1)
		require.Len(t, boards.Samples, 1)
		require.Equal(t, 100e12, modules.Samples[0].GetHashrateHs())
		require.Equal(t, 100e12, boards.Samples[0].GetHashrateHs())
		require.EqualValues(t, 3600, boards.Samples[0].ResolutionSeconds)
		require.Empty(t, modules.NextPageToken)
		require.Empty(t, boards.NextPageToken)
		for _, table := range []string{"container_module_diagnostic_observation", "hashboard_diagnostic_observation"} {
			var count int
			require.NoError(t, f.db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count))
			require.GreaterOrEqual(t, count, 30240, "numeric history reads must preserve diagnostic storage")
		}
	})
}
