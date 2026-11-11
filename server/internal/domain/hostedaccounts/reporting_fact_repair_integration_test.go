package hostedaccounts_test

import (
	"fmt"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestReportingFactDeletionRepairsDriftInBoundedBatches(t *testing.T) {
	for _, drift := range []string{"container_site", "missing_container", "account_policy", "representative"} {
		t.Run(drift, func(t *testing.T) {
			f := setup(t)
			account := f.client(t, "Bounded repair", f.user, hostedaccounts.AllCapabilities)
			actor, actorUUID := reportingBoundaryActors(t, f, account.AccountID)
			var site, other int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Original','original') RETURNING id`, f.org).Scan(&site))
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Other','other') RETURNING id`, f.org).Scan(&other))
			device := f.miningDevice(t, pb.DeviceClass_DEVICE_CLASS_CONTAINER)
			_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, device)
			require.NoError(t, err)
			config := reporting.Configuration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1}
			if drift == "representative" {
				var member string
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.user).Scan(&member))
				config.Representatives = []reporting.Representative{{UserID: member, CanSubmit: true, SourceRef: "reviewed"}}
			}
			at := time.Date(2026, 1, 1, 0, 0, 0, 123, time.UTC)
			revision := int64(0)
			for offset := 0; offset < 201; offset += 200 {
				changes := []reporting.FactMutation{}
				for i := offset; i < min(offset+200, 201); i++ {
					changes = append(changes, reportingUpsertForTest(t, reporting.ContractResult{ID: fmt.Sprintf("result-%03d", i), Metric: "availability", SiteID: site, ContainerID: device, Start: at, End: at.Add(time.Minute), Value: "99", Unit: "%", SourceRef: "source", CalculationRef: "reviewed"}))
				}
				m, err := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision, "valid evidence", &config, changes)
				require.NoError(t, err)
				revision = m.Revision
			}
			switch drift {
			case "container_site":
				_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, other, device)
			case "missing_container":
				_, err = f.db.ExecContext(t.Context(), `DELETE FROM device_pairing WHERE device_id=$1`, device)
				require.NoError(t, err)
				_, err = f.db.ExecContext(t.Context(), `DELETE FROM device WHERE id=$1`, device)
			case "account_policy":
				err = f.svc.ConfigureSites(t.Context(), f.user, f.org, account.AccountID, true, []int64{other})
			case "representative":
				_, err = f.db.ExecContext(t.Context(), `DELETE FROM hosted_account_membership WHERE account_id=$1 AND user_id=$2`, account.ID, f.user)
			}
			require.NoError(t, err)
			deletes := []reporting.FactMutation{}
			for i := range 200 {
				deletes = append(deletes, reporting.FactMutation{Kind: reporting.FactExternalResult, ID: fmt.Sprintf("result-%03d", i), Delete: true})
			}
			// Cleanup still needs authority over the entire previous scope.
			require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.ReportingAuthority{UserID: actorUUID, CanCreate: true, SiteIDs: []int64{other}}, 1))
			_, err = f.svc.MutateReportingInputs(t.Context(), actor, f.org, account.AccountID, revision, "unauthorized cleanup", nil, deletes)
			require.ErrorContains(t, err, "account access denied")
			_, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision-1, "stale cleanup", nil, deletes)
			require.ErrorContains(t, err, "refresh")
			// Neither a configuration update nor an upsert can use the exception.
			_, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision, "configuration and cleanup", &config, deletes)
			require.Error(t, err)
			mixed := append([]reporting.FactMutation(nil), deletes[:199]...)
			mixed = append(mixed, reportingUpsertForTest(t, reporting.Order{ID: "unrelated", Name: "Unrelated", Start: at, SourceRef: "signed"}))
			_, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision, "upsert and cleanup", nil, mixed)
			require.Error(t, err)
			// Audit failure must roll back even a repair of externally invalid rows.
			_, err = f.db.ExecContext(t.Context(), `CREATE FUNCTION reject_repair_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='save_reporting_inputs' THEN RAISE EXCEPTION 'repair audit rejected'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_repair_audit BEFORE INSERT ON activity_log FOR EACH ROW EXECUTE FUNCTION reject_repair_audit()`)
			require.NoError(t, err)
			_, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision, "audit rejected cleanup", nil, deletes)
			require.ErrorContains(t, err, "repair audit rejected")
			current, err := f.svc.ReportingInputsMetadata(t.Context(), f.host, f.org, account.AccountID)
			require.NoError(t, err)
			require.Equal(t, revision, current.Revision)
			require.EqualValues(t, 201, current.RegisterCounts[reporting.FactExternalResult])
			_, err = f.db.ExecContext(t.Context(), `DROP TRIGGER reject_repair_audit ON activity_log`)
			require.NoError(t, err)
			partial, err := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision, "first cleanup batch", nil, deletes)
			require.NoError(t, err)
			require.Equal(t, revision+1, partial.Revision)
			require.EqualValues(t, 1, partial.RegisterCounts[reporting.FactExternalResult])
			require.Equal(t, []int64{site}, partial.SiteIDs)
			var count int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT fact_count FROM hosted_reporting_fact_site WHERE account_id=$1 AND site_id=$2`, account.ID, site).Scan(&count))
			require.EqualValues(t, 1, count)
			final, err := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, partial.Revision, "last cleanup batch", nil, []reporting.FactMutation{{Kind: reporting.FactExternalResult, ID: "result-200", Delete: true}})
			require.NoError(t, err)
			require.Empty(t, final.SiteIDs)
			require.Empty(t, final.RegisterCounts)
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='save_reporting_inputs'`).Scan(&count))
			require.Equal(t, final.Revision, count)
		})
	}
}
