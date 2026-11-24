package hostedaccounts_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/authn"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func analysisContext(t *testing.T, f fixture, actor int64, account string) context.Context {
	t.Helper()
	ctx, err := hostedaccounts.WithRequestAccountID(authn.SetInfo(t.Context(), &session.Info{UserID: actor, OrganizationID: f.org, AuthMethod: session.AuthMethodSession}), account)
	require.NoError(t, err)
	return ctx
}

func TestAccountAnalysisPerformanceUsesHistoryAndRevokesSites(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	request := analysisRequest(a.AccountID, "miner_performance")
	ctx := analysisContext(t, f, f.user, a.AccountID)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Analysis site','analysis-site') RETURNING id`, f.org).Scan(&site))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, miner)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, a.AccountID, 1, selected(5, miner, 0)))
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE asset_id=$2 AND kind=5`, request.Start.AsTime(), miner)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,site_id) SELECT $1,device_identifier,100e12,3000,$2 FROM device WHERE id=$3`, request.Start.AsTime().Add(time.Minute), site, miner)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,site_id) SELECT clock_timestamp(),device_identifier,900e12,4000,$1 FROM device WHERE id=$2`, site, miner)
	require.NoError(t, err)
	missing := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, missing)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, a.AccountID, 1, selected(5, missing, 0)))
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE asset_id=$2 AND kind=5`, request.Start.AsTime(), missing)
	require.NoError(t, err)
	generated, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
	require.NoError(t, err)
	require.Equal(t, []int64{site}, generated.SiteIds)
	require.Contains(t, generated.Tables[0].Columns, "Observed hashing availability (%)")
	require.Contains(t, generated.Tables[0].Columns, "Hashing availability reporting")
	require.NotContains(t, generated.Tables[0].Columns, "Observed uptime (%)")
	require.Equal(t, "100", generated.Tables[0].Rows[0].Cells[4])
	require.Equal(t, "1", generated.Tables[0].Rows[0].Cells[5])
	require.Equal(t, "2", generated.Tables[0].Rows[0].Cells[1])
	require.Equal(t, "Unavailable", generated.Tables[0].Rows[0].Cells[8], "incomplete paired telemetry cannot supply estate efficiency")
	require.NoError(t, f.allocate(t, f.host, f.root, a.AccountID, 2, selected(5, miner, 1)))
	loaded, err := f.svc.AccountAnalysis(ctx, f.user, f.org, a.AccountID, generated.AnalysisId)
	require.NoError(t, err)
	require.True(t, proto.Equal(generated, loaded), "recall cannot rewrite a saved historical snapshot")
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_analysis SET snapshot='{}' WHERE analysis_id=$1`, generated.AnalysisId)
	require.Error(t, err, "database guards immutable snapshots")
	page, err := f.svc.AnalysisArtifactPage(ctx, f.user, f.org, a.AccountID, generated.AnalysisId, 0)
	require.NoError(t, err)
	digest := sha256.Sum256(page.Data)
	require.Equal(t, hex.EncodeToString(digest[:]), page.SHA256)
	require.Contains(t, string(page.Data), "Historical performance")
	require.Contains(t, string(page.Data), "Observed hashing availability (%)")
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, a.AccountID, true, nil))
	_, err = f.svc.AccountAnalysis(ctx, f.user, f.org, a.AccountID, generated.AnalysisId)
	require.Error(t, err)
	require.Error(t, f.svc.DeleteAnalysis(ctx, f.user, f.org, a.AccountID, generated.AnalysisId), "delete rechecks saved site scope")
	_, err = f.svc.AnalysisArtifactPage(ctx, f.user, f.org, a.AccountID, generated.AnalysisId, 0)
	require.Error(t, err)
	listed, err := f.svc.AccountAnalyses(ctx, f.user, f.org, &pb.ListAccountAnalysesRequest{AccountId: a.AccountID})
	require.NoError(t, err)
	require.Empty(t, listed.Analyses)
}

func TestAccountAnalysisEnergyUsesSavedInputsAndDoesNotProrate(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	ctx := analysisContext(t, f, f.user, a.AccountID)
	request := analysisRequest(a.AccountID, "energy_usage")
	// Meter reporting still consumes closed legacy module periods.
	request.Start = timestamppb.New(time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second))
	request.End = timestamppb.New(request.Start.AsTime().Add(24 * time.Hour))
	start, end := request.Start.AsTime(), request.End.AsTime()
	reportActors(t, f, a.AccountID)
	module, container := f.miningModule(t)
	var site, interval int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Meter site','meter-site') RETURNING id`, f.org).Scan(&site))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, container)
	require.NoError(t, err)
	f.observeModuleAtCurrentSite(t, module, container)
	require.NoError(t, f.allocate(t, f.host, f.root, a.AccountID, 1, selected(6, module, 0)))
	f.legacyModuleHistory(t)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1,ended_at=$2 WHERE asset_id=$3 AND kind=6`, start, end, module)
	require.NoError(t, err)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM hosted_account_asset_interval WHERE asset_id=$1 AND kind=6`, module).Scan(&interval))
	inputs := reportingInputs(t, f, a.AccountID, f.host)
	inputs.Configuration.SharedAllocationRef = "private allocation shares"
	inputs.Configuration.Prerequisites = []reporting.Prerequisite{{Key: "G3", Ready: true, Owner: "Meter owner", SourceRef: "private signed meter validation"}}
	inputs.Configuration.Sources = []reporting.Source{{ID: "meter", Metric: "meter", Certified: true, SourceRef: "private meter certificate"}}
	inputs.Orders = []reporting.Order{{ID: "order", Name: "Order", Start: start, SourceRef: "private order"}}
	inputs.Coverage = []reporting.CoverageSegment{{ID: "coverage", AllocationIntervalID: interval, Kind: 6, AssetID: module, SiteID: site, ContainerID: container, OrderID: "order", Start: start, End: end, CapacityTHS: "100", SourceRef: "private allocation worksheet"}}
	inputs.Meters = []reporting.MeterInterval{
		{ID: "first", MeterID: "private meter identifier", SourceID: "meter", SiteID: site, ContainerID: container, OrderID: "order", Start: start, End: start.Add(30 * time.Minute), KWh: "10.0000000000000000002", AllocationFraction: "0.25", Quality: "measured", SourceRef: "private interval worksheet"},
		{ID: "crossing", MeterID: "private meter identifier", SourceID: "meter", SiteID: site, ContainerID: container, OrderID: "order", Start: start.Add(30 * time.Minute), End: end.Add(30 * time.Minute), KWh: "90", Quality: "measured", SourceRef: "private interval worksheet"},
	}
	withoutInputs, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
	require.NoError(t, err)
	require.Equal(t, "Unavailable", withoutInputs.Tables[0].Rows[0].Cells[1])
	require.Empty(t, withoutInputs.Tables[1].Rows)
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, a.AccountID, 0, "Signed worksheet", inputs)
	require.NoError(t, err)
	request.RequestId = uuid.NewString()
	generated, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
	require.NoError(t, err)
	require.Equal(t, "2.50000000000000000005", generated.Tables[0].Rows[0].Cells[1], "crossing interval must not be prorated or summed")
	require.Equal(t, "provisional", generated.Tables[0].Rows[0].Cells[3])
	require.Len(t, generated.Tables[1].Rows, 1, "out-of-range raw consumption is not exposed in saved details")
	require.Equal(t, "2.50000000000000000005", generated.Tables[1].Rows[0].Cells[5])
	require.NotContains(t, generated.Tables[1].Columns, "Allocation share")
	require.NotContains(t, generated.String(), "10.0000000000000000002")
	encoded := generated.String()
	require.NotContains(t, encoded, "private")
	require.NotContains(t, encoded, "90 kWh")
	page, err := f.svc.AnalysisArtifactPage(ctx, f.user, f.org, a.AccountID, generated.AnalysisId, 0)
	require.NoError(t, err)
	require.NotContains(t, string(page.Data), ",90,", "unsupported full-interval reading must not leak in CSV")
	require.NotContains(t, string(page.Data), "10.0000000000000000002")
	require.Contains(t, string(page.Data), "2.50000000000000000005")
	require.NotContains(t, string(page.Data), "Allocation share")
	inputs.Coverage[0].Start = start.Add(15 * time.Minute)
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, a.AccountID, 1, "Corrected commercial coverage", inputs)
	require.NoError(t, err)
	request.RequestId = uuid.NewString()
	gap, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
	require.NoError(t, err)
	require.Equal(t, "Unavailable", gap.Tables[0].Rows[0].Cells[1])
	require.Empty(t, gap.Tables[1].Rows, "a meter spanning a commercial allocation gap is private even when the total is unavailable")
	gapCSV, err := f.svc.AnalysisArtifactPage(ctx, f.user, f.org, a.AccountID, gap.AnalysisId, 0)
	require.NoError(t, err)
	require.NotContains(t, string(gapCSV.Data), ",10,")
	require.NotContains(t, string(gapCSV.Data), ",90,")
	loaded, err := f.svc.AccountAnalysis(ctx, f.user, f.org, a.AccountID, withoutInputs.AnalysisId)
	require.NoError(t, err)
	require.True(t, proto.Equal(withoutInputs, loaded), "new saved evidence cannot change an older snapshot")
	// A recall closes the allocation while its commercial coverage stays open.
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET ended_at=$1 WHERE id=$2`, start.Add(20*time.Minute), interval)
	require.NoError(t, err)
	request.RequestId = uuid.NewString()
	_, err = f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
	require.ErrorContains(t, err, "commercial coverage exceeds its evidenced allocation interval")
	require.True(t, fleeterror.IsFailedPreconditionError(err))
}

func TestAccountAnalysisHistoricalSiteScopeAndUnplacedRevocation(t *testing.T) {
	for _, placed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unplaced", true: "moved site"}[placed], func(t *testing.T) {
			f := setup(t)
			a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			ctx := analysisContext(t, f, f.user, a.AccountID)
			request := analysisRequest(a.AccountID, "miner_performance")
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			var oldSite, newSite int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Old site','old-site') RETURNING id`, f.org).Scan(&oldSite))
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'New site','new-site') RETURNING id`, f.org).Scan(&newSite))
			if placed {
				_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, oldSite, miner)
				require.NoError(t, err)
			}
			require.NoError(t, f.allocate(t, f.host, f.root, a.AccountID, 1, selected(5, miner, 0)))
			_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE asset_id=$2 AND kind=5`, request.Start.AsTime(), miner)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,site_id) SELECT $1,device_identifier,100e12,3000,site_id FROM device WHERE id=$2`, request.Start.AsTime().Add(time.Minute), miner)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, newSite, miner)
			require.NoError(t, err)
			generated, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
			require.NoError(t, err)
			require.Equal(t, "100", generated.Tables[0].Rows[0].Cells[4])
			if placed {
				require.Equal(t, []int64{oldSite}, generated.SiteIds, "current placement cannot substitute for historical scope")
			} else {
				require.Empty(t, generated.SiteIds, "unplaced scope markers must not become displayed site IDs")
			}
			require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, a.AccountID, true, []int64{newSite}))
			_, err = f.svc.AccountAnalysis(ctx, f.user, f.org, a.AccountID, generated.AnalysisId)
			require.Error(t, err)
		})
	}
}

func TestAccountAnalysisRepairsPrivacyAndEveryChunkAdmission(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	ctx := analysisContext(t, f, f.user, a.AccountID)
	request := analysisRequest(a.AccountID, "issues_repairs")
	request.Name = "=SUM(1)"
	var sharedTicket int64
	for range 20 {
		ticket := createClientIssue(t, f, f.user, a.AccountID, strings.Repeat("Visible issue ", 280))
		sharedTicket = ticket.Ticket.Id
		_, err := f.db.ExecContext(t.Context(), `UPDATE repair_ticket SET created_at=$1,diagnosis='private diagnosis',notes='private host notes' WHERE id=$2`, request.Start.AsTime().Add(time.Minute), ticket.Ticket.Id)
		require.NoError(t, err)
	}
	hidden := createClientIssue(t, f, f.user, a.AccountID, "private unshared issue")
	_, err := f.db.ExecContext(t.Context(), `UPDATE repair_ticket SET created_at=$1,shared_with_clients=false WHERE id=$2`, request.Start.AsTime().Add(time.Minute), hidden.Ticket.Id)
	require.NoError(t, err)
	generated, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
	require.NoError(t, err)
	require.Len(t, generated.Tables[0].Rows, 20)
	require.NotContains(t, generated.String(), "private")
	require.Equal(t, "Current status", generated.Tables[0].Columns[1])
	_, err = f.db.ExecContext(t.Context(), `UPDATE repair_ticket SET shared_with_clients=false WHERE id=$1`, sharedTicket)
	require.NoError(t, err)
	_, err = f.svc.AccountAnalysis(ctx, f.user, f.org, a.AccountID, generated.AnalysisId)
	require.Error(t, err, "unsharing one captured ticket revokes the complete snapshot")
	_, err = f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
	require.Error(t, err, "idempotent retry rechecks the saved ticket scope")
	library, err := f.svc.AccountAnalyses(ctx, f.user, f.org, &pb.ListAccountAnalysesRequest{AccountId: a.AccountID})
	require.NoError(t, err)
	require.Empty(t, library.Analyses, "batch ticket privacy checks omit a newly private snapshot")
	_, err = f.db.ExecContext(t.Context(), `UPDATE repair_ticket SET shared_with_clients=true WHERE id=$1`, sharedTicket)
	require.NoError(t, err)
	page, err := f.svc.AnalysisArtifactPage(ctx, f.user, f.org, a.AccountID, generated.AnalysisId, 0)
	require.NoError(t, err)
	require.Greater(t, page.Chunks, int64(1))
	require.Contains(t, string(page.Data), "'=SUM(1)", "CSV must neutralize formula prefixes")
	disabled := false
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, a.AccountID, hostedaccounts.AllCapabilities, false, &disabled))
	_, err = f.svc.AnalysisArtifactPage(ctx, f.user, f.org, a.AccountID, generated.AnalysisId, 1)
	require.Error(t, err, "ticket admission is checked again for each chunk")
	_, err = f.svc.AccountAnalysis(ctx, f.user, f.org, a.AccountID, generated.AnalysisId)
	require.Error(t, err)
	listed, err := f.svc.AccountAnalyses(ctx, f.user, f.org, &pb.ListAccountAnalysesRequest{AccountId: a.AccountID})
	require.NoError(t, err)
	require.Empty(t, listed.Analyses)
}

func TestAccountAnalysisRejectsPreviewAPIKeysAndAccountSubstitution(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	b := f.client(t, "Other", f.user, hostedaccounts.AllCapabilities)
	ctx := analysisContext(t, f, f.user, a.AccountID)
	generated, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, analysisRequest(a.AccountID, "energy_usage"))
	require.NoError(t, err)
	_, err = f.svc.AccountAnalysis(ctx, f.user, f.org, b.AccountID, generated.AnalysisId)
	require.Error(t, err)
	_, err = f.svc.AccountAnalysis(analysisContext(t, f, f.user, b.AccountID), f.user, f.org, b.AccountID, generated.AnalysisId)
	require.Error(t, err)
	_, err = f.svc.GenerateAnalysis(analysisContext(t, f, f.host, a.AccountID), f.host, f.org, analysisRequest(a.AccountID, "energy_usage"))
	require.Error(t, err, "host preview cannot generate")
	_, err = f.svc.GenerateAnalysis(t.Context(), f.user, f.org, analysisRequest(a.AccountID, "energy_usage"))
	require.Error(t, err, "a real session and selected account are required")
	keyCtx := authn.SetInfo(ctx, &session.Info{UserID: f.user, OrganizationID: f.org, AuthMethod: session.AuthMethodAPIKey, AccountKey: true})
	_, err = f.svc.AccountAnalysis(keyCtx, f.user, f.org, a.AccountID, generated.AnalysisId)
	require.Error(t, err)
}

func analysisRequest(account, kind string) *pb.GenerateAccountAnalysisRequest {
	end := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	return &pb.GenerateAccountAnalysisRequest{RequestId: uuid.NewString(), AccountId: account, Name: "My analysis", Kind: kind, Start: timestamppb.New(end.Add(-time.Hour)), End: timestamppb.New(end), Timezone: "UTC"}
}

func TestAccountAnalysisSnapshotAndLiveMembership(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	ctx := analysisContext(t, f, f.user, a.AccountID)
	generated, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, analysisRequest(a.AccountID, "miner_performance"))
	require.NoError(t, err)
	require.NotEmpty(t, generated.AnalysisId)
	loaded, err := f.svc.AccountAnalysis(ctx, f.user, f.org, a.AccountID, generated.AnalysisId)
	require.NoError(t, err)
	require.True(t, proto.Equal(generated, loaded))
	listed, err := f.svc.AccountAnalyses(ctx, f.user, f.org, &pb.ListAccountAnalysesRequest{AccountId: a.AccountID})
	require.NoError(t, err)
	require.Len(t, listed.Analyses, 1)
	require.Empty(t, listed.Analyses[0].Tables)
	other := addUser(t, f.db, f.org)
	_, err = f.svc.AccountAnalysis(analysisContext(t, f, other, a.AccountID), other, f.org, a.AccountID, generated.AnalysisId)
	require.Error(t, err)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, a.AccountID, other, hostedaccounts.RoleViewer))
	_, err = f.svc.AccountAnalysis(analysisContext(t, f, other, a.AccountID), other, f.org, a.AccountID, generated.AnalysisId)
	require.NoError(t, err, "saved analyses are shared with account members")
	require.NoError(t, f.svc.RevokeMembership(t.Context(), f.user, f.org, a.AccountID, other))
	_, err = f.svc.AccountAnalysis(analysisContext(t, f, other, a.AccountID), other, f.org, a.AccountID, generated.AnalysisId)
	require.Error(t, err)
}

func TestAccountAnalysisGenerationIsIdempotentAndBoundToItsRequest(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	ctx := analysisContext(t, f, f.user, a.AccountID)
	request := analysisRequest(a.AccountID, "energy_usage")
	var wg sync.WaitGroup
	results := make([]*pb.AccountAnalysis, 8)
	errors := make([]error, len(results))
	for i := range results {
		wg.Go(func() { results[i], errors[i] = f.svc.GenerateAnalysis(ctx, f.user, f.org, request) })
	}
	wg.Wait()
	for i := range results {
		require.NoError(t, errors[i])
		require.True(t, proto.Equal(results[0], results[i]), "concurrent retries return the same frozen snapshot")
	}
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_analysis`).Scan(&count))
	require.Equal(t, 1, count)
	changed := proto.CloneOf(request)
	changed.Name = "Different request"
	_, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, changed)
	require.ErrorContains(t, err, "different request")
	changed.RequestId = uuid.NewString()
	_, err = f.svc.GenerateAnalysis(ctx, f.user, f.org, changed)
	require.NoError(t, err, "a new request ID may save another analysis")
	for _, id := range []string{"", "not-a-uuid"} {
		changed.RequestId = id
		_, err = f.svc.GenerateAnalysis(ctx, f.user, f.org, changed)
		require.ErrorContains(t, err, "UUID")
	}
	changed.RequestId, changed.Timezone = uuid.NewString(), "Local"
	_, err = f.svc.GenerateAnalysis(ctx, f.user, f.org, changed)
	require.ErrorContains(t, err, "timezone")
}

func TestAccountAnalysisLibraryPaginatesStableTiesAndSearchesOlderRows(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	ctx := analysisContext(t, f, f.user, a.AccountID)
	original, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, analysisRequest(a.AccountID, "miner_performance"))
	require.NoError(t, err)
	for i := range 101 {
		value := proto.CloneOf(original)
		value.AnalysisId, value.Kind, value.Name = uuid.NewString(), "energy_usage", fmt.Sprintf("Saved %03d", i)
		if i == 0 {
			value.Name = "Needle oldest analysis"
		}
		value.CreatedAt = timestamppb.New(original.CreatedAt.AsTime().Add(-time.Hour))
		snapshot, err := protojson.Marshal(value)
		require.NoError(t, err)
		require.NoError(t, sqlc.New(f.db).CreateAccountAnalysis(t.Context(), sqlc.CreateAccountAnalysisParams{AnalysisID: value.AnalysisId, OrganizationID: f.org, AccountID: a.ID, Kind: value.Kind, Snapshot: snapshot, SiteIds: []int64{}, TicketIds: []int64{}, CreatedBy: f.user, CreatedAt: value.CreatedAt.AsTime(), CsvData: []byte("fixture"), Sha256: "fixture", RequestKey: uuid.NewString(), RequestDigest: "fixture"}))
	}
	request := &pb.ListAccountAnalysesRequest{AccountId: a.AccountID}
	seen := map[string]bool{}
	for pageNumber := 0; ; pageNumber++ {
		page, err := f.svc.AccountAnalyses(ctx, f.user, f.org, request)
		require.NoError(t, err)
		if pageNumber < 2 {
			require.Len(t, page.Analyses, 50)
		} else {
			require.Len(t, page.Analyses, 2)
		}
		for _, value := range page.Analyses {
			require.False(t, seen[value.AnalysisId], "equal timestamps cannot duplicate a row across pages")
			seen[value.AnalysisId] = true
			require.Empty(t, value.Tables)
		}
		if page.NextPageToken == "" {
			break
		}
		if pageNumber == 0 {
			invalid := proto.CloneOf(request)
			invalid.Search, invalid.PageToken = "different filter", page.NextPageToken
			_, err := f.svc.AccountAnalyses(ctx, f.user, f.org, invalid)
			require.ErrorContains(t, err, "page token")
		}
		request.PageToken = page.NextPageToken
	}
	require.Len(t, seen, 102)
	for _, filter := range []*pb.ListAccountAnalysesRequest{{AccountId: a.AccountID, Search: "NEEDLE"}, {AccountId: a.AccountID, Kind: "miner_performance"}} {
		page, err := f.svc.AccountAnalyses(ctx, f.user, f.org, filter)
		require.NoError(t, err)
		require.Len(t, page.Analyses, 1, "filters run against the full saved library")
		require.Empty(t, page.NextPageToken)
	}
	page, err := f.svc.AccountAnalyses(ctx, f.user, f.org, &pb.ListAccountAnalysesRequest{AccountId: a.AccountID, PageSize: 100})
	require.NoError(t, err)
	require.Len(t, page.Analyses, 100)
	require.NotEmpty(t, page.NextPageToken)
	_, err = f.svc.AccountAnalyses(ctx, f.user, f.org, &pb.ListAccountAnalysesRequest{AccountId: a.AccountID, PageSize: 101})
	require.Error(t, err)
}

func TestAccountAnalysisMutationRequiresDirectAdministrator(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	ctx := analysisContext(t, f, f.user, a.AccountID)
	generated, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, analysisRequest(a.AccountID, "energy_usage"))
	require.NoError(t, err)
	for _, role := range []hostedaccounts.Role{hostedaccounts.RoleViewer, hostedaccounts.RolePoolOperator} {
		member := addUser(t, f.db, f.org)
		require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, a.AccountID, member, role))
		memberCtx := analysisContext(t, f, member, a.AccountID)
		_, err := f.svc.GenerateAnalysis(memberCtx, member, f.org, analysisRequest(a.AccountID, "energy_usage"))
		require.Error(t, err)
		require.Error(t, f.svc.DeleteAnalysis(memberCtx, member, f.org, a.AccountID, generated.AnalysisId))
		_, err = f.svc.AccountAnalysis(memberCtx, member, f.org, a.AccountID, generated.AnalysisId)
		require.NoError(t, err, "read authority remains available")
		_, err = f.svc.AnalysisArtifactPage(memberCtx, member, f.org, a.AccountID, generated.AnalysisId, 0)
		require.NoError(t, err)
		library, err := f.svc.AccountAnalyses(memberCtx, member, f.org, &pb.ListAccountAnalysesRequest{AccountId: a.AccountID})
		require.NoError(t, err)
		require.Len(t, library.Analyses, 1)
	}
	require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, a.AccountID, f.root, hostedaccounts.View|hostedaccounts.GenerateReports))
	preview := analysisContext(t, f, f.host, a.AccountID)
	previewAccess, err := f.svc.Resolve(preview, f.host, f.org, a.AccountID)
	require.NoError(t, err)
	require.True(t, previewAccess.Has(hostedaccounts.GenerateReports), "the capability alone does not replace direct administrator membership")
	_, err = f.svc.AccountAnalysis(preview, f.host, f.org, a.AccountID, generated.AnalysisId)
	require.NoError(t, err, "ancestor host authority can still preview")
	require.Error(t, f.svc.DeleteAnalysis(preview, f.host, f.org, a.AccountID, generated.AnalysisId))
	_, err = f.svc.GenerateAnalysis(preview, f.host, f.org, analysisRequest(a.AccountID, "energy_usage"))
	require.Error(t, err)
	other := f.client(t, "Other", f.user, hostedaccounts.AllCapabilities)
	require.Error(t, f.svc.DeleteAnalysis(analysisContext(t, f, f.user, other.AccountID), f.user, f.org, other.AccountID, generated.AnalysisId))
	keyCtx := authn.SetInfo(ctx, &session.Info{UserID: f.user, OrganizationID: f.org, AuthMethod: session.AuthMethodAPIKey, AccountKey: true})
	require.Error(t, f.svc.DeleteAnalysis(keyCtx, f.user, f.org, a.AccountID, generated.AnalysisId))
	require.Error(t, f.svc.DeleteAnalysis(t.Context(), f.user, f.org, a.AccountID, generated.AnalysisId))
	_, err = f.svc.AccountAnalysis(ctx, f.user, f.org, a.AccountID, generated.AnalysisId)
	require.NoError(t, err, "denied deletes leave the snapshot intact")
}

func TestAccountAnalysisPermanentDeletionRetainsOnlyReceiptAndBlocksReplay(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	ctx := analysisContext(t, f, f.user, a.AccountID)
	request := analysisRequest(a.AccountID, "energy_usage")
	request.Name = "PRIVATE ANALYSIS CONTENT"
	generated, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `DELETE FROM hosted_account_analysis WHERE analysis_id=$1`, generated.AnalysisId)
	require.Error(t, err, "raw deletion requires a matching retained receipt")
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_analysis SET snapshot='{}' WHERE analysis_id=$1`, generated.AnalysisId)
	require.Error(t, err)
	_, err = f.db.ExecContext(t.Context(), `TRUNCATE hosted_account_analysis`)
	require.Error(t, err)
	require.NoError(t, f.svc.DeleteAnalysis(ctx, f.user, f.org, a.AccountID, generated.AnalysisId))
	require.NoError(t, f.svc.DeleteAnalysis(ctx, f.user, f.org, a.AccountID, generated.AnalysisId), "retry uses retained deletion receipt")
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_analysis WHERE analysis_id=$1`, generated.AnalysisId).Scan(&count))
	require.Zero(t, count, "snapshot and CSV are physically removed")
	var receipt string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT row_to_json(d)::text FROM hosted_account_analysis_deletion d WHERE analysis_id=$1`, generated.AnalysisId).Scan(&receipt))
	require.Contains(t, receipt, request.RequestId)
	require.NotContains(t, receipt, request.Name)
	var forbiddenColumns int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM information_schema.columns WHERE table_name='hosted_account_analysis_deletion' AND column_name NOT IN ('analysis_id','organization_id','account_id','created_by','deleted_by','deleted_at','request_key','request_digest')`).Scan(&forbiddenColumns))
	require.Zero(t, forbiddenColumns, "receipt contains only IDs, actor/time, and idempotency metadata")
	_, err = f.svc.AccountAnalysis(ctx, f.user, f.org, a.AccountID, generated.AnalysisId)
	require.Error(t, err)
	_, err = f.svc.AnalysisArtifactPage(ctx, f.user, f.org, a.AccountID, generated.AnalysisId, 0)
	require.Error(t, err)
	library, err := f.svc.AccountAnalyses(ctx, f.user, f.org, &pb.ListAccountAnalysesRequest{AccountId: a.AccountID})
	require.NoError(t, err)
	require.Empty(t, library.Analyses)
	_, err = f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
	require.ErrorContains(t, err, "permanently deleted")
	for _, mutation := range []string{`DELETE FROM hosted_account_analysis_deletion`, `UPDATE hosted_account_analysis_deletion SET request_digest=''`, `TRUNCATE hosted_account_analysis_deletion`} {
		_, err = f.db.ExecContext(t.Context(), mutation)
		require.Error(t, err, "receipt is append-only")
	}
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_analysis(analysis_id,organization_id,account_id,kind,snapshot,site_ids,ticket_ids,created_by,csv_data,sha256,request_key,request_digest) VALUES($1,$2,$3,'energy_usage','{}','{}','{}',$4,'csv','digest',$5,'digest')`, uuid.NewString(), f.org, a.ID, f.user, request.RequestId)
	require.Error(t, err, "database guard prevents old request key resurrection")
	rollback, err := migrations.Current.ReadFile("current/001090_internal_account_analysis_deletion.down.sql")
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), string(rollback))
	require.ErrorContains(t, err, "deletion receipts prevent rollback")
	request.RequestId = uuid.NewString()
	_, err = f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
	require.NoError(t, err, "a deliberate new request can generate a new analysis")
}

func TestAccountAnalysisDeletionAuditFailureRollsBackReceiptAndContents(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	ctx := analysisContext(t, f, f.user, a.AccountID)
	generated, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, analysisRequest(a.AccountID, "energy_usage"))
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `CREATE FUNCTION fail_analysis_deletion_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='delete_account_analysis' THEN RAISE EXCEPTION 'deletion audit failed'; END IF; RETURN NEW; END $$; CREATE TRIGGER analysis_deletion_audit_failure BEFORE INSERT ON activity_log FOR EACH ROW EXECUTE FUNCTION fail_analysis_deletion_audit()`)
	require.NoError(t, err)
	require.Error(t, f.svc.DeleteAnalysis(ctx, f.user, f.org, a.AccountID, generated.AnalysisId))
	var contents, receipts int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_analysis`).Scan(&contents))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_analysis_deletion`).Scan(&receipts))
	require.Equal(t, 1, contents)
	require.Zero(t, receipts)
	_, err = f.db.ExecContext(t.Context(), `DROP TRIGGER analysis_deletion_audit_failure ON activity_log`)
	require.NoError(t, err)
	require.NoError(t, f.svc.DeleteAnalysis(ctx, f.user, f.org, a.AccountID, generated.AnalysisId))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='delete_account_analysis'`).Scan(&receipts))
	require.Equal(t, 1, receipts)
}

func TestAccountAnalysisEnergyFiltersModuleMappingsWithBoardCoverage(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Two sites", f.user, hostedaccounts.AllCapabilities)
	reportActors(t, f, account.AccountID)
	inputs := reviewedModuleInputs(t, f, account.AccountID)
	other := reviewedModuleInputs(t, f, account.AccountID)
	for i := range other.Coverage {
		other.Coverage[i].ID = "other-" + other.Coverage[i].ID
	}
	other.ModuleCoverage[0].ID = "other-module"
	for i := range other.ModuleCoverage[0].BoardCoverageIDs {
		other.ModuleCoverage[0].BoardCoverageIDs[i] = "other-" + other.ModuleCoverage[0].BoardCoverageIDs[i]
	}
	inputs.Obligations = nil
	inputs.Coverage = append(inputs.Coverage, other.Coverage...)
	inputs.ModuleCoverage = append(inputs.ModuleCoverage, other.ModuleCoverage...)
	inputs.Configuration.Prerequisites = []reporting.Prerequisite{{Key: "G3", Ready: true, Owner: "Meter owner", SourceRef: "signed validation"}}
	inputs.Configuration.Sources = []reporting.Source{{ID: "meter", Metric: "meter", Certified: true, SourceRef: "certificate"}}
	for _, m := range inputs.ModuleCoverage {
		inputs.Meters = append(inputs.Meters, reporting.MeterInterval{ID: m.ID, MeterID: m.ID, SourceID: "meter", SiteID: m.SiteID, ContainerID: m.ContainerID, OrderID: m.OrderID, Start: m.Start, End: m.End, KWh: "10", Quality: "measured", SourceRef: "meter worksheet"})
	}
	_, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "two-site sources", inputs)
	require.NoError(t, err)
	ctx := analysisContext(t, f, f.user, account.AccountID)
	request := analysisRequest(account.AccountID, "energy_usage")
	request.Start = timestamppb.New(inputs.ModuleCoverage[0].Start.Truncate(time.Second))
	request.End = timestamppb.Now()
	site := inputs.ModuleCoverage[0].SiteID
	request.SiteId = &site
	selected, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
	require.NoError(t, err)
	require.Len(t, selected.Tables[1].Rows, 1)
	require.Equal(t, "10", selected.Tables[0].Rows[0].Cells[1])
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, account.AccountID, true, []int64{site}))
	request.SiteId = nil
	request.RequestId = uuid.NewString()
	restricted, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
	require.NoError(t, err)
	require.Len(t, restricted.Tables[1].Rows, 1)
	require.Equal(t, "10", restricted.Tables[0].Rows[0].Cells[1])
}
