package hostedaccounts_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/block/proto-fleet/server/internal/domain/sites"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The test-local trigger pauses Save after validation while the real site service
// attempts to move its evidence. The permissive callback isolates storage fencing.
func TestReportingExternalFenceCurrentPlacementRace(t *testing.T) {
	for _, operation := range []string{"move", "delete-site", "delete-device"} {
		for _, fact := range []string{"coverage", "meter", "fee"} {
			t.Run(operation+"/"+fact, func(t *testing.T) {
				f := setup(t)
				account := f.client(t, "Placement race", f.user, hostedaccounts.AllCapabilities)
				reportingBoundaryActors(t, f, account.AccountID)
				input, _, _ := currentBoardReportingInputs(t, f, account.AccountID)
				if operation != "delete-device" {
					input.Coverage[0].End = time.Time{}
				} else {
					require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, 2, selected(7, input.Coverage[0].AssetID, 1)))
				}
				input.Hashrates = nil
				input.PoolObservations = nil
				c := input.Coverage[0]
				if fact != "coverage" {
					input.Coverage = nil
					// A current-only device witness has no retained allocation to lock.
					_, err := f.db.ExecContext(t.Context(), `DELETE FROM hosted_account_asset_interval WHERE device_id=$1`, c.ContainerID)
					require.NoError(t, err)
					if fact == "meter" {
						input.Meters = []reporting.MeterInterval{{ID: "meter", MeterID: "meter", SourceID: "meter-source", SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: c.Start, End: c.Start.Add(time.Hour), KWh: "1", Quality: "measured", SourceRef: "reading"}}
					}
					if fact == "fee" {
						input.Fees = []reporting.FeeLine{{ID: "fee", SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: c.Start, End: c.Start.Add(time.Hour), Amount: "1", Currency: "USD", SourceRef: "invoice"}}
					}
				}
				require.NoError(t, input.Validate())
				var identifier string
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT device_identifier FROM device WHERE id=$1`, c.ContainerID).Scan(&identifier))
				var target int64
				require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Race destination','race-destination') RETURNING id`, f.org).Scan(&target))
				gate := reportingInputSaveGate(t, f)
				ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
				defer cancel()
				saved := make(chan error, 1)
				go func() {
					_, e := saveReportingInputsForTest(f.svc, ctx, f.host, f.org, account.AccountID, 0, "race input", input)
					saved <- e
				}()
				reportingPID := waitReportingInputSaveGate(t, f, ctx)
				store := sqlstores.NewSQLSiteStore(f.db)
				siteSvc := sites.NewService(store, sqlstores.NewSQLBuildingStore(f.db), nil, nil, nil, sqlstores.NewSQLTransactor(f.db), nil)
				moved := make(chan error, 1)
				go func() {
					var e error
					switch operation {
					case "move":
						_, e = siteSvc.AssignContainersToSite(ctx, f.org, &target, []string{identifier}, func(context.Context, *int64) error { return nil })
					case "delete-site":
						_, e = siteSvc.DeleteSite(ctx, f.org, c.SiteID)
					case "delete-device":
						// Exercise the actual deletion UPDATE and placement trigger. The
						// service's independent configuration-ownership admission can reject
						// this CT1 fixture before it reaches physical placement.
						_, e = sqlc.New(f.db).SoftDeleteDevices(ctx, sqlc.SoftDeleteDevicesParams{DeviceIdentifiers: []string{identifier}, OrgID: f.org})
					}
					moved <- e
				}()
				// A blocked device-row lock proves move cannot invalidate validation before
				// the reporting transaction commits. Observe the database, not elapsed time.
				blocked := false
				require.Eventually(t, func() bool {
					select {
					case e := <-moved:
						require.NoError(t, e)
						return true
					default:
					}
					var waiting bool
					err := f.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))`, reportingPID).Scan(&waiting)
					blocked = err == nil && waiting
					return blocked
				}, 3*time.Second, 10*time.Millisecond)
				require.NoError(t, gate.Commit())
				select {
				case e := <-saved:
					require.NoError(t, e)
				case <-ctx.Done():
					t.Fatal("save did not finish after releasing the gate")
				}

				require.True(t, blocked, "placement must wait for reporting commit")
				select {
				case e := <-moved:
					require.NoError(t, e)
				case <-ctx.Done():
					t.Fatal("move did not finish after save")
				}
				_, err := saveReportingInputsForTest(f.svc, ctx, f.host, f.org, account.AccountID, 1, "revalidate", input)
				if operation == "delete-device" {
					require.NoError(t, err, "retained historical witnesses survive soft deletion")
				} else if fact == "coverage" {
					require.ErrorContains(t, err, "commercial coverage exceeds its evidenced allocation interval")
				} else {
					require.ErrorContains(t, err, "reporting container is outside the evidenced organization and site")
				}
			})
		}
	}
}

func TestReportingExternalFenceBusyDeviceReturnsConflict(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Busy device", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input, _, _ := currentBoardReportingInputs(t, f, account.AccountID)
	c := input.Coverage[0]
	_, other := f.miningModule(t)
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, c.SiteID, other)
	require.NoError(t, err)
	input.ExternalResults = []reporting.ContractResult{{ID: "additional-device", Metric: "availability", SiteID: c.SiteID, ContainerID: other, Start: c.Start, End: c.Start.Add(time.Hour), Value: "99", Unit: "%", SourceRef: "source", CalculationRef: "calculation"}}
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), `SELECT id FROM device WHERE id=$1 FOR UPDATE`, other)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err = saveReportingInputsForTest(f.svc, ctx, f.host, f.org, account.AccountID, 0, "busy", input)
	require.ErrorContains(t, err, "reporting history changed")
	require.NoError(t, ctx.Err(), "NOWAIT must finish before the caller deadline")
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_reporting_inputs`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='save_reporting_inputs'`).Scan(&count))
	require.Zero(t, count)
	// Reporting locked the first device before encountering the busy second one.
	// Its failed transaction must release that partial fence, allowing a bulk
	// physical writer holding the second device to acquire the first immediately.
	_, err = tx.ExecContext(t.Context(), `SELECT id FROM device WHERE id=$1 FOR UPDATE NOWAIT`, c.ContainerID)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "retry", input)
	require.NoError(t, err, "failed reporting transaction releases all admission locks")
}

// The trigger gates only this organization, so it cannot fabricate global
// serialization when a test checks unrelated organizations.
func reportingInputSaveGate(t *testing.T, f fixture) *sql.Tx {
	t.Helper()
	_, err := f.db.ExecContext(t.Context(), fmt.Sprintf(`CREATE FUNCTION reporting_external_fence_gate() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.organization_id=%d THEN PERFORM pg_advisory_xact_lock(19761,1); END IF; RETURN NEW; END $$;
 CREATE TRIGGER reporting_external_fence_gate BEFORE INSERT ON hosted_reporting_inputs FOR EACH ROW EXECUTE FUNCTION reporting_external_fence_gate()`, f.org))
	require.NoError(t, err)
	gate, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = gate.Rollback() })
	_, err = gate.ExecContext(t.Context(), `SELECT pg_advisory_xact_lock(19761,1)`)
	require.NoError(t, err)
	return gate
}

func waitReportingInputSaveGate(t *testing.T, f fixture, ctx context.Context) int {
	t.Helper()
	var pid int
	require.Eventually(t, func() bool {
		e := f.db.QueryRowContext(ctx, `SELECT pid FROM pg_locks WHERE locktype='advisory' AND classid=19761 AND objid=1 AND NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database())`).Scan(&pid)
		return e == nil
	}, 3*time.Second, 10*time.Millisecond)
	return pid
}

func TestReportingExternalFenceCancellationReleasesDevicesAndOtherOrganizationsProgress(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Canceled report", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input, _, _ := currentBoardReportingInputs(t, f, account.AccountID)
	// Build another organization in the same database before holding admission.
	other := fixture{db: f.db, svc: f.svc}
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO organization(org_id,name) VALUES($1,'Other reporting organization') RETURNING id`, uuid.NewString()).Scan(&other.org))
	require.NoError(t, authz.Reconcile(t.Context(), f.db))
	other.host = addUser(t, f.db, other.org)
	other.user = addUser(t, f.db, other.org)
	var role int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM role WHERE organization_id=$1 AND builtin_key='SUPER_ADMIN'`, other.org).Scan(&role))
	_, err := sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: other.host, OrganizationID: other.org, RoleID: role, ScopeType: "org"})
	require.NoError(t, err)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT account_id FROM hosted_account WHERE organization_id=$1 AND depth=0`, other.org).Scan(&other.root))
	otherAccount := other.client(t, "Independent report", other.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, other, otherAccount.AccountID)
	gate := reportingInputSaveGate(t, f)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	saveCtx, cancelSave := context.WithCancel(ctx)
	defer cancelSave()
	done := make(chan error, 1)
	go func() {
		_, e := saveReportingInputsForTest(f.svc, saveCtx, f.host, f.org, account.AccountID, 0, "cancel", input)
		done <- e
	}()
	reportingPID := waitReportingInputSaveGate(t, f, ctx)
	_, err = saveReportingInputsForTest(other.svc, ctx, other.host, other.org, otherAccount.AccountID, 0, "independent", reportingInputs(t, other, otherAccount.AccountID, other.host))
	require.NoError(t, err)
	cancelSave()
	select {
	case err = <-done:
		require.Error(t, err)
	case <-ctx.Done():
		t.Fatal("canceled save did not return")
	}
	// database/sql may return from cancellation while its rollback goroutine
	// drains the canceled connection. Observe the actual transaction ending.
	require.Eventually(t, func() bool {
		var ended bool
		e := f.db.QueryRowContext(t.Context(), `SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND xact_start IS NOT NULL)`, reportingPID).Scan(&ended)
		return e == nil && ended
	}, time.Second, 10*time.Millisecond)
	tx, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `SELECT id FROM device WHERE id=$1 FOR UPDATE NOWAIT`, input.Coverage[0].ContainerID)
	require.NoError(t, err, "cancellation releases the physical fence")
	require.NoError(t, tx.Rollback())
	var count int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM hosted_reporting_inputs WHERE organization_id=$1`, f.org).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM activity_log WHERE event_type='save_reporting_inputs' AND metadata->>'account_id'=$1`, account.AccountID).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, gate.Commit())
	_, err = saveReportingInputsForTest(f.svc, ctx, f.host, f.org, account.AccountID, 0, "retry", input)
	require.NoError(t, err)
}

func TestReportingExternalWitnessDriftRejectsUnrelatedEditAndAllowsAtomicRemoval(t *testing.T) {
	for _, kind := range []reporting.FactKind{reporting.FactCoverage, reporting.FactMeter, reporting.FactFee} {
		t.Run(string(kind), func(t *testing.T) {
			f := setup(t)
			account := f.client(t, "Drift removal", f.user, hostedaccounts.AllCapabilities)
			reportingBoundaryActors(t, f, account.AccountID)
			input, _, _ := currentBoardReportingInputs(t, f, account.AccountID)
			c := input.Coverage[0]
			input.Hashrates = nil
			input.PoolObservations = nil
			id := c.ID
			if kind == reporting.FactCoverage {
				input.Coverage[0].End = time.Time{}
			} else {
				input.Coverage = nil
				_, e := f.db.ExecContext(t.Context(), `DELETE FROM hosted_account_asset_interval WHERE device_id=$1`, c.ContainerID)
				require.NoError(t, e)
				id = "current-only"
				if kind == reporting.FactMeter {
					input.Meters = []reporting.MeterInterval{{ID: id, MeterID: "meter", SourceID: "meter", SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: c.Start, End: c.End, KWh: "1", Quality: "measured", SourceRef: "reading"}}
				} else {
					input.Fees = []reporting.FeeLine{{ID: id, SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: c.Start, End: c.End, Amount: "1", Currency: "USD", SourceRef: "invoice"}}
				}
			}
			saved, e := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "saved witness", input)
			require.NoError(t, e)
			var identifier string
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT device_identifier FROM device WHERE id=$1`, c.ContainerID).Scan(&identifier))
			var target int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Destination','drift-destination') RETURNING id`, f.org).Scan(&target))
			siteSvc := sites.NewService(sqlstores.NewSQLSiteStore(f.db), sqlstores.NewSQLBuildingStore(f.db), nil, nil, nil, sqlstores.NewSQLTransactor(f.db), nil)
			_, e = siteSvc.AssignContainersToSite(t.Context(), f.org, &target, []string{identifier}, func(context.Context, *int64) error { return nil })
			require.NoError(t, e)
			unrelated := reportingUpsertForTest(t, reporting.Order{ID: "unrelated", Name: "Unrelated", Start: c.Start, SourceRef: "order"})
			_, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "unrelated edit", nil, []reporting.FactMutation{unrelated})
			require.Error(t, e)
			m, e := f.svc.ReportingInputsMetadata(t.Context(), f.host, f.org, account.AccountID)
			require.NoError(t, e)
			require.Equal(t, saved.Revision, m.Revision)
			require.EqualValues(t, 1, m.RegisterCounts[reporting.FactOrder])
			corrected, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "remove invalid saved witness", nil, []reporting.FactMutation{unrelated, {Kind: kind, ID: id, Delete: true}})
			require.NoError(t, e)
			require.Empty(t, corrected.SiteIDs)
			require.EqualValues(t, 2, corrected.RegisterCounts[reporting.FactOrder])
		})
	}
}
