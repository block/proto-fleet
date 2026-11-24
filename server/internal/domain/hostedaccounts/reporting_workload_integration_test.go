package hostedaccounts_test

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

// This is an opt-in characterization of the real synchronous service. The facts
// are daily/monthly aggregates, and the monitoring samples are deliberately sparse;
// neither models full-cadence telemetry or certifies a production estate.
func TestHostedReportingWorkloadLifecycle(t *testing.T) {
	if os.Getenv("HOSTED_REPORT_WORKLOAD") != "1" {
		t.Skip("set HOSTED_REPORT_WORKLOAD=1 to measure the real-DB reporting lifecycle")
	}
	polls := 20
	if raw := os.Getenv("HOSTED_REPORT_WORKLOAD_POLLS"); raw != "" {
		var err error
		polls, err = strconv.Atoi(raw)
		require.NoError(t, err)
		require.Contains(t, []int{20, 200, 1000}, polls, "explicit sampled-density profiles only")
	}
	for _, annual := range []bool{false, true} {
		name := "monthly_30_days"
		if annual {
			name = "annual_with_prior_year"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Synthetic workload client", f.user, hostedaccounts.AllCapabilities)
			owner, _ := reportActors(t, f, client.AccountID)
			p := reportPeriod()
			if annual {
				p.Kind = "annual"
				p.Start = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
				p.End = p.Start.AddDate(1, 0, 0)
			}
			in := workloadReportingInputs(t, f, client, p, annual)
			facts := len(in.Orders) + len(in.Coverage) + len(in.ModuleCoverage) + len(in.Hashrates) + len(in.Meters) + len(in.Fees) + len(in.Incidents) + len(in.Obligations) + len(in.PoolObservations) + len(in.ExternalResults)
			expectedFacts := 9356
			if annual {
				expectedFacts = 7645
			}
			require.Equal(t, expectedFacts, facts)
			encoded, err := json.Marshal(in)
			require.NoError(t, err)
			t.Logf("estate=1 CT1/6 tanks/96 modules/288 boards/1 hosting order; facts=%d input_json_bytes=%d; retained_sample_polls=%d", facts, len(encoded), polls)
			corrected := workloadMonitoringHours(t, f, in, p, polls)
			captured := polls * len(in.Coverage)
			daily := reporting.DailyObservations(p, in.Coverage)
			observationRows := int64(captured)
			if daily {
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(DISTINCT (interval_id, date_trunc('day', bucket, 'UTC'))) FROM hosted_account_reporting_hour`).Scan(&observationRows))
			}

			start := time.Now()
			saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "Synthetic aggregate worksheet; no source certification", in)
			saveTime := time.Since(start)
			t.Logf("bounded_mutations=%s (5s per transaction)", saveTime)
			require.NoError(t, err)
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT clock_timestamp()`).Scan(&p.AsOf))
			start = time.Now()
			draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner})
			prepareTime := time.Since(start)
			t.Logf("prepare=%s (60s transaction budget)", prepareTime)
			require.NoError(t, err)
			result, err := json.Marshal(draft.Result)
			require.NoError(t, err)
			require.Len(t, draft.Result.Containers, 1)
			require.Len(t, draft.Result.Orders, 1)
			if annual {
				require.Equal(t, "12000", draft.Result.Metrics["year_over_year_energy"].Value)
				require.Equal(t, "5760000000000000", draft.Result.Metrics["year_over_year_hashrate"].Value)
			}
			// A later revision has a bucket inside the period but cannot enter a
			// report whose evidence cutoff preceded this write, including at publication.
			_, err = f.db.ExecContext(t.Context(), workloadHourInsert+` SELECT interval_id,bucket,3 AS revision FROM hosted_account_reporting_hour WHERE revision=2) h`)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			_, err = f.svc.PublishReport(ctx, f.host, f.org, client.AccountID, draft.ReportID, draft.Revision, "Synthetic source limitations")
			require.Error(t, err)
			var artifacts, issues int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact WHERE report_internal_id=$1`, draft.ID).Scan(&artifacts))
			require.Zero(t, artifacts)
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='publish_report'`).Scan(&issues))
			require.Zero(t, issues)
			start = time.Now()
			issued, err := f.svc.PublishReport(t.Context(), f.host, f.org, client.AccountID, draft.ReportID, draft.Revision, "Synthetic engineering workload only; contractual formulas, source certification and commissioning remain unavailable")
			publishTime := time.Since(start)
			t.Logf("publish=%s (60s transaction budget); result_json_bytes=%d/%d; breakdowns=2/%d", publishTime, len(result), reporting.MaxResultJSONBytes, reporting.MaxReportBreakdowns)
			require.NoError(t, err)
			require.Equal(t, "issued", issued.Status)
			require.False(t, issued.Result.Complete)
			frozen := frozenReportEvidence(t, f, issued.ID)
			require.Len(t, frozen, captured)
			require.Contains(t, frozen, corrected, "the correction captured before the cutoff is frozen")
			for _, line := range frozen {
				require.False(t, strings.HasSuffix(line, " 3"), "a revision written after the cutoff cannot enter the report")
			}
			t.Logf("frozen_monitoring_hours=%d; daily_observations=%t; observation_rows=%d; post_cutoff_revisions_excluded=1", len(frozen), daily, observationRows)
			for _, name := range []string{"report.html", "evidence.zip", "source-inputs.json"} {
				actor := f.user
				if name == "source-inputs.json" {
					actor = f.host
				}
				path := workloadDownloadArtifact(t, f, actor, client.AccountID, issued.ReportID, name)
				if name == "evidence.zip" {
					member := "observations.csv"
					if daily {
						member = "observation_days.csv"
					}
					workloadVerifyZIP(t, path, member, observationRows)
				}
				if name == "source-inputs.json" && annual {
					source, err := os.Open(path)
					require.NoError(t, err)
					var frozen struct {
						Inputs reporting.Inputs `json:"inputs"`
					}
					require.NoError(t, json.NewDecoder(source).Decode(&frozen))
					require.NoError(t, source.Close())
					require.Len(t, frozen.Inputs.Hashrates, 6912, "host source retains both comparison years")
					require.Len(t, frozen.Inputs.Meters, 24)
				}
			}
		})
	}
}

func workloadReportingInputs(t *testing.T, f fixture, client hostedaccounts.Account, p reporting.Period, annual bool) reporting.Inputs {
	t.Helper()
	start := p.Start
	if annual {
		start = start.AddDate(-1, 0, 0)
	}
	historyEpoch(t, f, start.Add(-time.Hour))
	controller := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Synthetic workload site','synthetic-workload') RETURNING id`, f.org).Scan(&site))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, controller)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_inventory(device_id,org_id,observed_at) VALUES($1,$2,$3)`, controller, f.org, p.End)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `WITH tanks AS (
 INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) SELECT $1,n,$2,$3 FROM generate_series(1,6) n RETURNING id
) INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at,state,state_observed_at)
 SELECT t.id,n,$2,$3,'active',$3 FROM tanks t CROSS JOIN generate_series(1,16) n`, controller, start, p.End)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `WITH boards AS (
 INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) SELECT $1,$2,$3 FROM generate_series(1,288) RETURNING id
), numbered AS (SELECT id,row_number() OVER(ORDER BY id)-1 AS n FROM boards), modules AS (
 SELECT m.id,m.tank_id,m.position,row_number() OVER(ORDER BY t.position,m.position)-1 AS n FROM container_module m JOIN container_tank t ON t.id=m.tank_id WHERE t.device_id=$4
) INSERT INTO hashboard_installation(hashboard_id,module_id,position,site_id,device_id,tank_id,module_position,installed_at,last_seen_at)
 SELECT b.id,m.id,(b.n%3)+1,$5,$4,m.tank_id,m.position,$2,$3 FROM numbered b JOIN modules m ON m.n=b.n/3`, f.org, start, p.End, controller, site)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,hashboard_id,client_id)
 SELECT $1,7,hashboard_id,$2,hashboard_id,$3 FROM hashboard_installation WHERE device_id=$2`, f.org, controller, client.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE device_id=$2`, start, controller)
	require.NoError(t, err)
	in := reportingInputs(t, f, client.AccountID, f.host)
	in.Configuration.Sources = []reporting.Source{{ID: "synthetic-hash", Metric: "hashrate", SourceRef: "Synthetic aggregate intervals; uncertified"}, {ID: "synthetic-meter", Metric: "meter", SourceRef: "Synthetic aggregate meter; uncertified"}}
	in.Orders = []reporting.Order{{ID: "synthetic-order", Name: "Synthetic hosting order", Start: start, End: p.End, SourceRef: "Synthetic order fixture; no signed production contract"}}
	rows, err := f.db.QueryContext(t.Context(), `SELECT a.id,a.asset_id,i.module_id FROM hosted_account_asset_interval a JOIN hashboard_installation i ON i.hashboard_id=a.asset_id WHERE a.device_id=$1 AND a.kind=7 ORDER BY i.module_id,i.position`, controller)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	modules := map[int64][]string{}
	var moduleOrder []int64
	for rows.Next() {
		var interval, board, module int64
		require.NoError(t, rows.Scan(&interval, &board, &module))
		id := fmt.Sprintf("board-%d", board)
		if len(modules[module]) == 0 {
			moduleOrder = append(moduleOrder, module)
		}
		modules[module] = append(modules[module], id)
		in.Coverage = append(in.Coverage, reporting.CoverageSegment{ID: id, AllocationIntervalID: interval, Kind: 7, AssetID: board, SiteID: site, ContainerID: controller, OrderID: "synthetic-order", Start: start, End: p.End, CapacityTHS: "100", SourceRef: "Synthetic historical board allocation"})
		in.PoolObservations = append(in.PoolObservations, reporting.PoolObservation{ID: id, Kind: 7, AssetID: board, SourceID: "synthetic-pool", SourceAt: p.End.Add(-time.Minute), ReceivedAt: p.End, State: "unknown", SourceRef: "Synthetic pool sample; uncertified"})
		for at, index := start, 0; at.Before(p.End); index++ {
			next := at.AddDate(0, 0, 1)
			if annual {
				next = at.AddDate(0, 1, 0)
			}
			value := "100000000000000"
			if at.Before(p.Start) {
				value = "80000000000000"
			}
			in.Hashrates = append(in.Hashrates, reporting.HashrateInterval{ID: fmt.Sprintf("%s-%d", id, index), Kind: 7, AssetID: board, SiteID: site, ContainerID: controller, OrderID: "synthetic-order", Start: at, End: next, HashrateHS: value, SourceID: "synthetic-hash", Complete: true, SourceRef: "Synthetic aggregate hash interval"})
			at = next
		}
	}
	require.NoError(t, rows.Err())
	for _, module := range moduleOrder {
		in.ModuleCoverage = append(in.ModuleCoverage, reporting.ModuleCoverageSegment{ID: fmt.Sprint(module), ModuleID: module, SiteID: site, ContainerID: controller, OrderID: "synthetic-order", Start: start, End: p.End, BoardCoverageIDs: modules[module], WholeModule: true, SourceRef: "Synthetic qualified historical placement"})
	}
	for at, index := start, 0; at.Before(p.End); index++ {
		next := at.AddDate(0, 0, 1)
		if annual {
			next = at.AddDate(0, 1, 0)
		}
		energy := "2000"
		if at.Before(p.Start) {
			energy = "1000"
		}
		in.Meters = append(in.Meters, reporting.MeterInterval{ID: fmt.Sprint(index), MeterID: "synthetic-meter", SourceID: "synthetic-meter", SiteID: site, ContainerID: controller, OrderID: "synthetic-order", Start: at, End: next, KWh: energy, Quality: "measured", SourceRef: "Synthetic aggregate meter interval"})
		if annual {
			in.Fees = append(in.Fees, reporting.FeeLine{ID: fmt.Sprint(index), SiteID: site, ContainerID: controller, OrderID: "synthetic-order", Start: at, End: next, Amount: "100", Currency: "USD", SourceRef: "Synthetic fee ledger"})
		}
		at = next
	}
	if !annual {
		in.Fees = []reporting.FeeLine{{ID: "monthly", SiteID: site, ContainerID: controller, OrderID: "synthetic-order", Start: start, End: p.End, Amount: "100", Currency: "USD", SourceRef: "Synthetic fee ledger"}}
	}
	for i := range 4 {
		at := p.Start.Add(time.Duration(i+1) * 24 * time.Hour)
		in.Incidents = append(in.Incidents, reporting.Incident{ID: fmt.Sprint(i), SiteID: site, ContainerID: controller, OrderID: "synthetic-order", Start: at, End: at.Add(time.Hour), AsOf: p.End, Category: "maintenance", Summary: "Synthetic maintenance sample", SourceRef: "Synthetic incident register"})
	}
	for i := range 8 {
		at := p.Start.Add(time.Duration(i+1) * 24 * time.Hour)
		in.Obligations = append(in.Obligations, reporting.ObligationInterval{ID: fmt.Sprint(i), EpisodeID: fmt.Sprint(i), Kind: 6, AssetID: moduleOrder[i], SiteID: site, ContainerID: controller, OrderID: "synthetic-order", Start: at, End: at.Add(time.Hour), AsOf: p.End, ReceivedAt: at, Qualified: true, SourceRef: "Synthetic reviewed module repair sample"})
	}
	return in
}

const workloadHourInsert = `INSERT INTO hosted_account_reporting_hour(interval_id,bucket,revision,capture_reason,no_source_data,window_start,window_end,
 hashrate_report_count,hashrate_count,hashrate_positive_count,hashrate_sum,hashrate_complete,hashrate_source_oldest_at,hashrate_source_newest_at,
 power_report_count,power_count,temperature_report_count,temperature_count,state_report_count,state_count)
 SELECT interval_id,bucket,revision,'closed',false,bucket,bucket+interval '1 hour',1,1,1,100e12,true,bucket,bucket+interval '59 minutes',0,0,0,0,0,0 FROM (`

// workloadMonitoringHours writes polls sampled closed hours for every board
// allocation, then corrects one hour, and returns that correction's frozen line.
func workloadMonitoringHours(t *testing.T, f fixture, in reporting.Inputs, p reporting.Period, polls int) string {
	t.Helper()
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_reporting_allocation_snapshot(interval_id,organization_id,client_id,
 subclient_id,site_id,kind,asset_id,device_id,device_identifier,started_at)
 SELECT id,organization_id,client_id,subclient_id,site_id,kind,asset_id,device_id,device_identifier,started_at
 FROM hosted_account_asset_interval WHERE device_id=$1 ON CONFLICT(interval_id) DO NOTHING`, in.Coverage[0].ContainerID)
	require.NoError(t, err)
	step := max(p.End.Sub(p.Start)/time.Duration(polls+1), time.Hour).Truncate(time.Hour)
	_, err = f.db.ExecContext(t.Context(), workloadHourInsert+` SELECT id AS interval_id,$1::timestamptz+n*$2::interval AS bucket,1 AS revision
 FROM hosted_account_asset_interval CROSS JOIN generate_series(1,$3) n WHERE device_id=$4`+`) h`, p.Start, step.String(), polls, in.Coverage[0].ContainerID)
	require.NoError(t, err)
	c := in.Coverage[0]
	bucket := p.Start.Add(step)
	_, err = f.db.ExecContext(t.Context(), workloadHourInsert+` SELECT $1::bigint,$2::timestamptz,2) h(interval_id,bucket,revision)`, c.AllocationIntervalID, bucket)
	require.NoError(t, err)
	return fmt.Sprintf("%d %s closed 2", c.AllocationIntervalID, bucket.UTC().Format(time.RFC3339))
}

func workloadDownloadArtifact(t *testing.T, f fixture, actor int64, account, report, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	file, err := os.Create(path)
	require.NoError(t, err)
	digest := sha256.New()
	start := time.Now()
	first, err := f.svc.ReportArtifactPage(t.Context(), actor, f.org, account, report, name, 0, name == "source-inputs.json")
	require.NoError(t, err)
	var size int64
	for sequence := range first.Chunks {
		page := first
		if sequence > 0 {
			page, err = f.svc.ReportArtifactPage(t.Context(), actor, f.org, account, report, name, sequence, name == "source-inputs.json")
			require.NoError(t, err)
		}
		require.Equal(t, first.AuthorityFingerprint, page.AuthorityFingerprint)
		n, err := io.MultiWriter(file, digest).Write(page.Data)
		require.NoError(t, err)
		size += int64(n)
	}
	require.NoError(t, file.Close())
	require.Equal(t, first.Bytes, size)
	require.Equal(t, first.SHA256, hex.EncodeToString(digest.Sum(nil)))
	t.Logf("download=%s duration=%s bytes=%d chunks=%d sha256=%s", name, time.Since(start), size, first.Chunks, first.SHA256)
	return path
}

func workloadVerifyZIP(t *testing.T, path, member string, observations int64) {
	t.Helper()
	archive, err := zip.OpenReader(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, archive.Close()) }()
	files := map[string]*zip.File{}
	for _, file := range archive.File {
		require.NotContains(t, files, file.Name)
		files[file.Name] = file
	}
	manifestFile, err := files["manifest.json"].Open()
	require.NoError(t, err)
	var manifest struct {
		SchemaVersion string `json:"schemaVersion"`
		Files         []struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
			Bytes  int64  `json:"bytes"`
			Rows   int64  `json:"rows"`
		} `json:"files"`
	}
	require.NoError(t, json.NewDecoder(manifestFile).Decode(&manifest))
	require.NoError(t, manifestFile.Close())
	require.Equal(t, len(archive.File)-1, len(manifest.Files))
	foundObservations := false
	seen := map[string]bool{}
	for _, entry := range manifest.Files {
		require.NotEqual(t, "manifest.json", entry.Name)
		require.False(t, seen[entry.Name], "manifest member repeated: %s", entry.Name)
		seen[entry.Name] = true
		file := files[entry.Name]
		require.NotNil(t, file)
		reader, err := file.Open()
		require.NoError(t, err)
		digest := sha256.New()
		require.GreaterOrEqual(t, entry.Bytes, int64(0))
		size, err := io.Copy(digest, io.LimitReader(reader, entry.Bytes+1))
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		require.Equal(t, entry.Bytes, size, entry.Name)
		require.Equal(t, entry.SHA256, hex.EncodeToString(digest.Sum(nil)), entry.Name)
		if strings.HasSuffix(entry.Name, ".csv") {
			reader, err := file.Open()
			require.NoError(t, err)
			csvReader := csv.NewReader(reader)
			_, err = csvReader.Read()
			require.NoError(t, err)
			var count int64
			for {
				_, err := csvReader.Read()
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				count++
			}
			require.NoError(t, reader.Close())
			require.Equal(t, entry.Rows, count, entry.Name)
			if entry.Name == member {
				foundObservations = true
				require.Equal(t, observations, count)
				t.Logf("zip_profile=%s observations_rows=%d observations_uncompressed_bytes=%d members=%d", manifest.SchemaVersion, count, size, len(archive.File))
			}
		}
	}
	for name := range files {
		if name != "manifest.json" {
			require.True(t, seen[name], "archive member missing from manifest: %s", name)
		}
	}
	require.True(t, foundObservations)
}

func TestHostedReportingWorkloadInputBudgetsPreserveCurrentRecord(t *testing.T) {
	f, client, _, in, p := reportContextFixture(t)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "Original current record", in)
	require.NoError(t, err)
	snapshot := func() string {
		var value string
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT jsonb_build_object('header',(SELECT to_jsonb(h) FROM hosted_reporting_inputs h WHERE account_id=$1),'facts',(SELECT jsonb_agg(to_jsonb(f) ORDER BY row_id) FROM hosted_reporting_fact f WHERE account_id=$1),'audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY event_id) FROM activity_log a))::text`, client.ID).Scan(&value))
		return value
	}
	c := in.Coverage[0]
	for _, bytes := range []bool{false, true} {
		name := "mutation_count"
		if bytes {
			name = "mutation_bytes"
		}
		t.Run(name, func(t *testing.T) {
			changes := []reporting.FactMutation{}
			expected := "mutation count"
			if bytes {
				expected = "byte budget"
				for i := range 2 {
					changes = append(changes, reportingUpsertForTest(t, reporting.PoolObservation{ID: fmt.Sprintf("large-%d", i), Kind: c.Kind, AssetID: c.AssetID, SourceID: "pool", SourceAt: p.Start, ReceivedAt: p.Start, State: "unknown", SourceRef: "reviewed pool", Reason: strings.Repeat("r", 600000)}))
				}
			} else {
				for i := range reporting.MaxFactMutationsPerRequest + 1 {
					changes = append(changes, reportingUpsertForTest(t, reporting.FeeLine{ID: fmt.Sprintf("fee-%03d", i), SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: p.Start, End: p.End, Amount: "1", Currency: "USD", SourceRef: "reviewed fee"}))
				}
			}
			before := snapshot()
			_, err := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, client.AccountID, saved.Revision, "Reject atomic oversized request", nil, changes)
			require.ErrorContains(t, err, expected)
			require.Equal(t, before, snapshot(), "request exhaustion cannot change facts, revision, scope or audit")
			metadata, err := f.svc.ReportingInputsMetadata(t.Context(), f.host, f.org, client.AccountID)
			require.NoError(t, err)
			require.Equal(t, saved.ReportingInputsMetadata, metadata)
			_, err = f.svc.ReportingFactPage(t.Context(), f.host, f.org, client.AccountID, reporting.FactCoverage, saved.Revision, "", 201)
			require.ErrorContains(t, err, "invalid reporting register page")
			page, err := f.svc.ReportingFactPage(t.Context(), f.host, f.org, client.AccountID, reporting.FactCoverage, saved.Revision, "", 1)
			require.NoError(t, err)
			require.Len(t, page.Facts, 1)
		})
	}
}

func TestHostedReportingLargeAccountLifecycle(t *testing.T) {
	for _, relevant := range []bool{false, true} {
		name, total := "accumulated_history", 12000
		if relevant {
			name, total = "relevant_fee_stream", 10201
		}
		t.Run(name, func(t *testing.T) {
			f, account, owner, in, p := reportContextFixture(t)
			saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "baseline", in)
			require.NoError(t, err)
			baseline, err := reporting.CalculateContext(t.Context(), p, in)
			require.NoError(t, err)
			revision := saved.Revision
			c := in.Coverage[0]
			source := strings.Repeat("s", 1000)
			start := time.Now()
			for offset := 0; offset < total; offset += reporting.MaxFactMutationsPerRequest {
				changes := []reporting.FactMutation{}
				for i := offset; i < min(offset+reporting.MaxFactMutationsPerRequest, total); i++ {
					a, b := p.Start, p.End
					if !relevant {
						a, b = p.Start.AddDate(-1, 0, 0), p.Start.AddDate(-1, 0, 0).Add(time.Hour)
					}
					changes = append(changes, reportingUpsertForTest(t, reporting.FeeLine{ID: fmt.Sprintf("scale-%05d", i), SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: a, End: b, Amount: "1.000000000000000001", Currency: "USD", SourceRef: source}))
				}
				metadata, err := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision, "bounded scale fees", nil, changes)
				require.NoError(t, err)
				require.Equal(t, revision+1, metadata.Revision)
				revision = metadata.Revision
			}
			t.Logf("facts=%d bounded_mutation_duration=%s", total+2, time.Since(start))
			count, canonicalBytes, pages := 0, 0, 0
			for _, kind := range reportingFixtureKinds {
				cursor, last := "", ""
				for {
					page, err := f.svc.ReportingFactPage(t.Context(), f.host, f.org, account.AccountID, kind, revision, cursor, hostedaccounts.MaxReportingFactPageSize)
					require.NoError(t, err)
					require.Equal(t, revision, page.Revision)
					require.LessOrEqual(t, len(page.Facts), hostedaccounts.MaxReportingFactPageSize)
					encoded, err := json.Marshal(page)
					require.NoError(t, err)
					require.LessOrEqual(t, len(encoded), hostedaccounts.MaxReportingFactPageBytes)
					for _, fact := range page.Facts {
						require.Greater(t, fact.ID, last, "pages must traverse every identity once in C order")
						require.Equal(t, kind, fact.Kind)
						last = fact.ID
						count++
						canonicalBytes += len(fact.Payload)
					}
					pages++
					if page.NextCursor == "" {
						break
					}
					require.NotEqual(t, cursor, page.NextCursor)
					cursor = page.NextCursor
				}
			}
			require.Equal(t, total+2, count)
			require.Greater(t, canonicalBytes, 8<<20)
			t.Logf("complete_register_traversal pages=%d canonical_bytes=%d", pages, canonicalBytes)
			start = time.Now()
			draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: revision, OwnerUserID: owner})
			require.NoError(t, err)
			t.Logf("prepare=%s", time.Since(start))
			if relevant {
				require.Equal(t, "10201.000000000000010201", draft.Result.Metrics["accrued_hosting_fees"].Amounts[0].Value)
			} else {
				requireReportingResultEqual(t, baseline, draft.Result)
			}
			start = time.Now()
			issued, err := f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, draft.ReportID, draft.Revision, "reviewed synthetic source limitations")
			require.NoError(t, err)
			require.Equal(t, "issued", issued.Status)
			t.Logf("publish=%s", time.Since(start))
			public, err := f.svc.AccountReport(t.Context(), f.user, f.org, account.AccountID, issued.ReportID)
			require.NoError(t, err)
			require.Equal(t, issued.Result.Metrics["accrued_hosting_fees"], public.Result.Metrics["accrued_hosting_fees"])
			_, err = f.svc.ReportArtifactPage(t.Context(), f.user, f.org, account.AccountID, issued.ReportID, "source-inputs.json", 0, false)
			require.Error(t, err, "host source inputs cannot become a client artifact")
			for _, name := range []string{"report.html", "evidence.zip", "source-inputs.json"} {
				actor := f.user
				if name == "source-inputs.json" {
					actor = f.host
				}
				path := workloadDownloadArtifact(t, f, actor, account.AccountID, issued.ReportID, name)
				switch name {
				case "evidence.zip":
					workloadVerifyZIP(t, path, "observations.csv", 0)
				case "report.html":
					data, err := os.ReadFile(path)
					require.NoError(t, err)
					require.NotContains(t, strings.ToLower(string(data)), "<script")
				case "source-inputs.json":
					file, err := os.Open(path)
					require.NoError(t, err)
					stat, err := file.Stat()
					require.NoError(t, err)
					var envelope struct {
						Inputs reporting.Inputs `json:"inputs"`
					}
					require.NoError(t, json.NewDecoder(file).Decode(&envelope))
					require.NoError(t, file.Close())
					if relevant {
						require.Greater(t, stat.Size(), int64(8<<20))
						require.Len(t, envelope.Inputs.Fees, total)
						for i, v := range envelope.Inputs.Fees {
							require.Equal(t, fmt.Sprintf("scale-%05d", i), v.ID)
							require.Equal(t, "1.000000000000000001", v.Amount)
							require.Equal(t, source, v.SourceRef)
							require.Equal(t, p.Start, v.Start)
							require.Equal(t, p.End, v.End)
						}
					} else {
						require.Empty(t, envelope.Inputs.Fees, "irrelevant lifetime history is absent from selected source artifact")
						require.Less(t, stat.Size(), int64(16<<10))
					}
				}
			}
		})
	}
}
