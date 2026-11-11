package hostedaccounts_test

import (
	"context"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestReportingFactWritesShareAdmissionAndExcludeRevocation(t *testing.T) {
	for _, revoke := range []string{"reporting authority", "role assignment"} {
		t.Run(revoke, func(t *testing.T) {
			f := setup(t)
			a := f.client(t, "Held write", f.user, hostedaccounts.AllCapabilities)
			b := f.client(t, "Independent account", f.user, hostedaccounts.AllCapabilities)
			reportingBoundaryActors(t, f, a.AccountID)
			reviewer, _ := reportingBoundaryActors(t, f, b.AccountID)
			config := reporting.Configuration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1}
			for _, account := range []string{a.AccountID, b.AccountID} {
				_, err := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account, 0, "initialize", &config, nil)
				require.NoError(t, err)
			}
			_, err := f.db.ExecContext(t.Context(), `CREATE FUNCTION gate_reporting_fact() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.fact_id='held' THEN PERFORM pg_advisory_xact_lock(19059,1); END IF; RETURN NEW; END $$; CREATE TRIGGER gate_reporting_fact AFTER INSERT ON hosted_reporting_fact FOR EACH ROW EXECUTE FUNCTION gate_reporting_fact()`)
			require.NoError(t, err)
			gate, err := f.db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer gate.Rollback()
			_, err = gate.ExecContext(t.Context(), `SELECT pg_advisory_xact_lock(19059,1)`)
			require.NoError(t, err)
			held := reportingUpsertForTest(t, reporting.Order{ID: "held", Name: "Held", Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), SourceRef: "signed"})
			written := make(chan error, 1)
			go func() {
				_, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, a.AccountID, 1, "held write", nil, []reporting.FactMutation{held})
				written <- e
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				e := f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND classid=19059 AND objid=1 AND NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`).Scan(&waiting)
				return e == nil && waiting
			}, 3*time.Second, 10*time.Millisecond)
			// Both the same actor and a different actor can read and write B
			// while A holds all of its admission locks and its account header.
			for i, actor := range []int64{f.host, reviewer} {
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				_, err = f.svc.ReportingInputsMetadata(ctx, actor, f.org, b.AccountID)
				cancel()
				require.NoError(t, err)
				ctx, cancel = context.WithTimeout(t.Context(), time.Second)
				change := reportingUpsertForTest(t, reporting.Order{ID: "independent", Name: "Independent", Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), SourceRef: "signed"})
				_, err = f.svc.MutateReportingInputs(ctx, actor, f.org, b.AccountID, int64(i+1), "independent write", nil, []reporting.FactMutation{change})
				cancel()
				require.NoError(t, err)
			}
			revoked := make(chan error, 1)
			query := "%LockHostedAccountOrganization%"
			if revoke == "reporting authority" {
				var userID string
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&userID))
				go func() {
					revoked <- f.svc.SetReportingAuthority(t.Context(), f.host, f.org, a.AccountID, hostedaccounts.ReportingAuthority{UserID: userID}, 1)
				}()
			} else {
				query = "DELETE FROM user_organization_role%"
				go func() {
					_, e := f.db.ExecContext(t.Context(), `DELETE FROM user_organization_role WHERE user_id=$1 AND organization_id=$2`, f.host, f.org)
					revoked <- e
				}()
			}
			require.Eventually(t, func() bool {
				var waiting bool
				e := f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND query LIKE $1 AND wait_event_type='Lock')`, query).Scan(&waiting)
				return e == nil && waiting
			}, time.Second, 10*time.Millisecond)
			require.NoError(t, gate.Commit())
			require.NoError(t, <-written)
			require.NoError(t, <-revoked)
			_, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, a.AccountID, 2, "after revocation", nil, []reporting.FactMutation{held})
			require.True(t, fleeterror.IsForbiddenError(err), "%v", err)
			var revision int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT revision FROM hosted_reporting_inputs WHERE account_id=$1`, a.ID).Scan(&revision))
			require.EqualValues(t, 2, revision)
		})
	}
}
