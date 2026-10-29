package hostedaccounts

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// These intervals contain established ownership only, never a projection of
// today's ledger into older telemetry. The same typed key is used throughout.
type dashboardInterval struct {
	IntervalID int64      `json:"interval_id"`
	Kind       int32      `json:"kind"`
	AssetID    int64      `json:"asset_id"`
	Identifier string     `json:"device_identifier"`
	Start      time.Time  `json:"started_at"`
	End        *time.Time `json:"ended_at"`
	SiteID     int64      `json:"site_id"`
}
type dashboardKey struct {
	kind int32
	id   int64
}

// Recent short windows retain exact raw bounds. Longer/older windows use
// allocation-aware summaries, rounded up to their source resolution.
const (
	dashboardRawResolution             = time.Duration(0)
	dashboardHourlyResolution          = time.Hour
	dashboardDailyResolution           = 24 * time.Hour
	dashboardHourlyRefreshCadence      = 30 * time.Minute
	dashboardDailyRefreshCadence       = 6 * time.Hour
	dashboardRawMaxRange               = dashboardDailyResolution
	dashboardHourlyMaxRange            = 10 * dashboardDailyResolution
	dashboardHistoryRetention          = 10 * dashboardDailyResolution
	dashboardHourlyRetentionMonths     = 3
	dashboardDailyRetentionYears       = 3
	dashboardHistoryMaxRange           = 366 * dashboardDailyResolution
	dashboardMaxBuckets                = 1500
	dashboardMinBucketWidth            = time.Minute
	dashboardModuleCollectionAllowance = 2*time.Minute + 5*time.Second
)

func dashboardHistoryResolution(now, start, end time.Time, bucketSeconds int32) (time.Duration, time.Duration) {
	step := time.Duration(bucketSeconds) * time.Second
	var resolution time.Duration
	if end.Sub(start) <= dashboardRawMaxRange && !start.Before(now.Add(-dashboardHistoryRetention)) {
		return dashboardRawResolution, step
	}
	if end.Sub(start) <= dashboardHourlyMaxRange && !start.Before(now.AddDate(0, -dashboardHourlyRetentionMonths, 0)) {
		resolution = dashboardHourlyResolution
	} else {
		resolution = dashboardDailyResolution
	}
	if remainder := step % resolution; remainder != 0 {
		step += resolution - remainder
	}
	return resolution, step
}

// A source-period summary is already an asset/output bucket only when the
// request does not merge periods or multiple allocation intervals for an asset.
func dashboardDirectSummaryBuckets(intervals []dashboardInterval, start time.Time, resolution, step time.Duration) bool {
	if resolution == dashboardRawResolution || step != resolution || !start.Equal(start.Truncate(resolution)) {
		return false
	}
	seen := make(map[dashboardKey]struct{}, len(intervals))
	for _, interval := range intervals {
		key := dashboardKey{interval.Kind, interval.AssetID}
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

// Each source bucket contains one wide summary row per allocation.
// Include the policy-normal daily-to-hourly rollup (up to 54h) for 2,500
// year-long allocations, separately from the raw poll-density budget.
const dashboardSummaryBudget = 1_100_000

func dashboardSummaryWork(intervals []dashboardInterval, start, end time.Time, resolution time.Duration) int64 {
	var work int64
	for _, interval := range intervals {
		if first, last, ok := interval.overlap(start, end); ok {
			work += int64((last.Sub(first)+resolution-1)/resolution) + 1
		}
		if work > dashboardSummaryBudget {
			break
		}
	}
	return work
}

// Limit normalized raw-tail work separately from materialized source buckets.
const dashboardSummaryTailBudget = 300 * 24 * time.Hour

func dashboardHealthyRetainedWork(intervals []dashboardInterval, start, end time.Time, resolution time.Duration, now time.Time) (int64, time.Duration) {
	hourly := maxTime(start, now.Add(-2*dashboardHourlyResolution-dashboardHourlyRefreshCadence))
	daily := maxTime(start, now.Add(-2*dashboardDailyResolution-dashboardDailyRefreshCadence))
	return dashboardRetainedWork(intervals, start, end, resolution, hourly, daily)
}

// A daily real-time read rolls up hourly rows above its watermark, then reads
// raw samples only above both watermarks. Count that hourly work as well as the
// saved daily buckets; neither refresh cadence is a bound on actual lag.
func dashboardRetainedWork(intervals []dashboardInterval, start, end time.Time, resolution time.Duration, hourly, daily time.Time) (int64, time.Duration) {
	rawStart := maxTime(start, hourly)
	var summaryWork int64
	if resolution == dashboardDailyResolution {
		hourlyStart := maxTime(start, daily)
		summaryWork = dashboardSummaryWork(intervals, start, minTime(end, hourlyStart), dashboardDailyResolution) +
			dashboardSummaryWork(intervals, hourlyStart, end, dashboardHourlyResolution)
		rawStart = maxTime(rawStart, hourlyStart)
	} else {
		summaryWork = dashboardSummaryWork(intervals, start, end, resolution)
	}
	return summaryWork, dashboardSummaryTailWork(intervals, rawStart, end)
}

func dashboardSummaryTailWork(intervals []dashboardInterval, start, end time.Time) time.Duration {
	var work time.Duration
	for _, interval := range intervals {
		if first, last, ok := interval.overlap(start, end); ok {
			work += last.Sub(first)
		}
		if work > dashboardSummaryTailBudget {
			break
		}
	}
	return work
}

// Bound raw work before scanning telemetry. Module observations expand into
// independently authorized contributor windows and cost more than miner rows.
// A dense compressed fixture measures 260 weighted asset-days below the 5s
// transaction deadline; leave headroom for estate authorization and cold reads.
const dashboardHistoryBudget = 300 * 24 * time.Hour
const dashboardModuleWeight = 8

func dashboardHistoryWork(intervals []dashboardInterval, start, end time.Time) time.Duration {
	var work time.Duration
	for _, interval := range intervals {
		first, last, ok := interval.overlap(start, end)
		if !ok {
			continue
		}
		duration := last.Sub(first)
		if interval.Kind == int32(pb.MiningAssetKind_MINING_ASSET_KIND_MODULE) {
			// The collection prefilter includes contributor freshness/clock-skew edges.
			duration = (duration + dashboardModuleCollectionAllowance) * dashboardModuleWeight
		}
		work += duration
		if work > dashboardHistoryBudget {
			return work
		}
	}
	return work
}

func (i dashboardInterval) overlap(start, end time.Time) (time.Time, time.Time, bool) {
	first, last := maxTime(start, i.Start), end
	if i.End != nil {
		last = minTime(last, *i.End)
	}
	return first, last, last.After(first)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (e miningEstate) dashboardOwner(owner *string) (int64, error) {
	if owner == nil {
		return 0, nil
	}
	if !e.management() {
		return 0, denied()
	}
	if *owner == e.account.AccountID {
		if e.account.Depth == 2 {
			return e.account.ID, nil
		}
		return 0, nil
	}
	for _, child := range e.children {
		if child.AccountID == *owner {
			return child.ID, nil
		}
	}
	return 0, denied()
}
func (e miningEstate) dashboardSite(site int64) bool {
	return siteAllowed(e.policy, site) && e.physicalRead(sqlc.ListHostedAccountMiningAssetsRow{SiteID: site})
}
func (e miningEstate) physicalReadAuthority() (json.RawMessage, error) {
	type siteScope struct {
		OrgWide bool    `json:"org_wide"`
		Sites   []int64 `json:"sites"`
	}
	authority := []siteScope{}
	if e.hostPermissions != nil {
		for _, permission := range []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead} {
			wide, sites := e.hostPermissions.SiteScopeFor(permission)
			if sites == nil {
				sites = []int64{}
			}
			authority = append(authority, siteScope{OrgWide: wide, Sites: sites})
		}
	}
	encoded, err := json.Marshal(authority)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("encode physical read authority: %w", err)
	}
	return encoded, nil
}

func (s *Service) Dashboard(ctx context.Context, actorID, orgID int64, request *pb.GetAccountDashboardRequest) (*pb.GetAccountDashboardResponse, error) {
	if request == nil || request.StartTime == nil || request.EndTime == nil || request.StartTime.CheckValid() != nil || request.EndTime.CheckValid() != nil {
		return nil, fleeterror.NewInvalidArgumentError("valid dashboard time range required")
	}
	// PostgreSQL timestamps have microsecond precision. Use the same origin for
	// SQL buckets and the in-memory expected-asset denominators.
	start, end := request.StartTime.AsTime().Truncate(time.Microsecond), request.EndTime.AsTime().Truncate(time.Microsecond)
	step := time.Duration(request.BucketSeconds) * time.Second
	if !end.After(start) || end.Sub(start) > dashboardHistoryMaxRange || step < dashboardMinBucketWidth || step > dashboardDailyResolution || request.SiteId != nil && *request.SiteId <= 0 {
		return nil, fleeterror.NewInvalidArgumentErrorf("dashboard range must be at most %d days with bucket widths from one minute to one day", dashboardHistoryMaxRange/dashboardDailyResolution)
	}
	resolution, step := dashboardHistoryResolution(time.Now().UTC(), start, end, request.BucketSeconds)
	if resolution != dashboardRawResolution {
		// Retained buckets represent whole UTC source periods. Align expected
		// allocations to those same periods, including the intersecting edges.
		start = start.Truncate(resolution)
	}
	if (end.Sub(start)+step-1)/step > dashboardMaxBuckets {
		return nil, fleeterror.NewInvalidArgumentErrorf("dashboard range must contain at most %d buckets", dashboardMaxBuckets)
	}
	effectiveBucketSeconds := int32(step / time.Second) //nolint:gosec // G115: validated widths round up to at most one day
	ctx, cancel := context.WithTimeout(ctx, miningTransactionTimeout)
	defer cancel()
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (*pb.GetAccountDashboardResponse, error) {
		e, err := loadMiningEstate(ctx, q, actorID, orgID, request.AccountId, true, miningRead)
		if err != nil {
			return nil, err
		}
		if e.account.Depth == 0 {
			return nil, denied()
		}
		owner, err := e.dashboardOwner(request.OwnerAccountId)
		if err != nil {
			return nil, err
		}
		response := &pb.GetAccountDashboardResponse{GeneratedAt: timestamppb.Now()}
		epoch, err := q.GetHostedAccountHistoryEpoch(ctx)
		if err != nil {
			return nil, err
		}
		retainedFrom := response.GeneratedAt.AsTime().Add(-dashboardHistoryRetention)
		if resolution != dashboardRawResolution {
			summaryEpoch, err := q.GetHostedAccountHistorySummaryEpoch(ctx)
			if err != nil {
				return nil, err
			}
			epoch = maxTime(epoch, summaryEpoch)
			retainedFrom = response.GeneratedAt.AsTime().AddDate(-dashboardDailyRetentionYears, 0, 0)
			if resolution == dashboardHourlyResolution {
				retainedFrom = response.GeneratedAt.AsTime().AddDate(0, -dashboardHourlyRetentionMonths, 0)
			}
		}
		if epoch.Before(retainedFrom) {
			epoch = retainedFrom
		}
		response.HistoryAvailableFrom = timestamppb.New(epoch)
		for _, child := range e.children {
			response.Children = append(response.Children, &pb.AccountMiningBucket{AccountId: child.AccountID, Name: child.Name, Suspended: child.Suspended})
		}
		totals := newMiningAccumulator()
		sites := map[int64]*miningAccumulator{}
		siteNames := map[int64]string{}
		for _, row := range e.rows {
			if !e.physicalRead(row) {
				continue
			}
			if request.OwnerAccountId != nil && row.SubclientID != owner {
				continue
			}
			asset := e.asset(row)
			if request.SiteId == nil || row.SiteID == *request.SiteId {
				totals.add(asset)
			}
			// Keep allocated capacity visible after policy narrows, with the same
			// redaction as inventory. Site identity and history remain policy-bound.
			if asset.OutsidePolicy {
				continue
			}
			if sites[row.SiteID] == nil {
				sites[row.SiteID] = newMiningAccumulator()
				siteNames[row.SiteID] = row.SiteName
			}
			sites[row.SiteID].add(asset)
		}
		response.Totals = totals.result()
		periods, err := q.ListHostedAccountHistoryIntervals(ctx, sqlc.ListHostedAccountHistoryIntervalsParams{OrganizationID: orgID, AccountID: e.account.ID, StartTime: start, EndTime: end, HasOwnerFilter: request.OwnerAccountId != nil, OwnerID: owner})
		if err != nil {
			return nil, err
		}
		intervals := []dashboardInterval{}
		for _, period := range periods {
			if !e.dashboardSite(period.SiteID) {
				continue
			}
			if sites[period.SiteID] == nil {
				sites[period.SiteID] = newMiningAccumulator()
			}
			if request.SiteId != nil && period.SiteID != *request.SiteId {
				continue
			}
			interval := dashboardInterval{IntervalID: period.ID, Kind: period.Kind, AssetID: period.AssetID, Identifier: period.DeviceIdentifier, Start: period.StartedAt, SiteID: period.SiteID}
			if period.EndedAt.Valid {
				t := period.EndedAt.Time
				interval.End = &t
			}
			intervals = append(intervals, interval)
		}
		siteIDs := []int64{}
		for id := range sites {
			if id > 0 {
				siteIDs = append(siteIDs, id)
			}
		}
		named, err := q.ListHostedAccountHistoricalSites(ctx, sqlc.ListHostedAccountHistoricalSitesParams{OrgID: orgID, SiteIds: siteIDs})
		if err != nil {
			return nil, err
		}
		for _, site := range named {
			siteNames[site.ID] = site.Name
		}
		sort.Slice(siteIDs, func(i, j int) bool { return siteIDs[i] < siteIDs[j] })
		for _, id := range siteIDs {
			response.Sites = append(response.Sites, &pb.AccountDashboardSite{SiteId: id, Name: siteNames[id], Summary: sites[id].result()})
		}
		var overBudget bool
		if resolution == dashboardRawResolution {
			overBudget = dashboardHistoryWork(intervals, start, end) > dashboardHistoryBudget
		} else {
			available := maxTime(start, epoch)
			// SQL includes the whole intersecting source bucket. Charge its tail
			// through now, while preserving the bounds of future-dated requests.
			budgetEnd := end
			if boundary := end.Truncate(resolution); !boundary.Equal(end) {
				budgetEnd = maxTime(end, minTime(boundary.Add(resolution), response.GeneratedAt.AsTime()))
			}
			// A healthy policy still leaves a real-time tail. Requests that cannot
			// fit that steady-state work need narrowing, not a retry after refresh.
			summaryWork, tailWork := dashboardHealthyRetainedWork(intervals, available, budgetEnd, resolution, response.GeneratedAt.AsTime())
			overBudget = summaryWork > dashboardSummaryBudget || tailWork > dashboardSummaryTailBudget
			if !overBudget && len(intervals) > 0 && budgetEnd.After(available) {
				watermarks, err := q.GetHostedAccountHistoryWatermarks(ctx, available)
				if err != nil {
					return nil, err
				}
				summaryWork, tailWork := dashboardRetainedWork(intervals, available, budgetEnd, resolution, watermarks.Hourly, watermarks.Daily)
				if summaryWork > dashboardSummaryBudget || tailWork > dashboardSummaryTailBudget {
					return nil, fleeterror.NewUnavailableErrorf("Hosted history refresh is behind for this request; retry after refresh catches up, or choose a shorter range or fewer allocations or sites")
				}
			}
		}
		if overBudget {
			return nil, fleeterror.NewInvalidArgumentError("This history request covers too many assets; choose a shorter time range or filter to fewer allocations or sites")
		}
		encoded, err := json.Marshal(intervals)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("encode allocation intervals: %w", err)
		}
		authority, err := e.physicalReadAuthority()
		if err != nil {
			return nil, err
		}
		var readings []sqlc.ListHostedAccountHistoricalReadingsRow
		if resolution == dashboardRawResolution {
			readings, err = q.ListHostedAccountHistoricalReadings(ctx, sqlc.ListHostedAccountHistoricalReadingsParams{OrganizationID: orgID, Intervals: encoded, StartTime: start, EndTime: end, BucketSeconds: effectiveBucketSeconds, SiteFilter: request.GetSiteId(), PolicyRestricted: e.policy.EffectiveRestricted, PolicySites: e.policy.EffectiveSiteIDs, HostAuthority: authority})
			if err != nil {
				return nil, err
			}
		} else {
			intervalIDs := make([]int64, len(intervals))
			for i, interval := range intervals {
				intervalIDs[i] = interval.IntervalID
			}
			if err := q.UseHostedAccountHistoryCustomPlan(ctx); err != nil {
				return nil, err
			}
			retained, err := q.ListHostedAccountRetainedReadings(ctx, sqlc.ListHostedAccountRetainedReadingsParams{DirectBuckets: dashboardDirectSummaryBuckets(intervals, start, resolution, step), AvailableFrom: epoch, Intervals: encoded, IntervalIds: intervalIDs, StartTime: start, EndTime: end, Hourly: resolution == dashboardHourlyResolution, BucketSeconds: effectiveBucketSeconds, SiteFilter: request.GetSiteId(), PolicyRestricted: e.policy.EffectiveRestricted, PolicySites: e.policy.EffectiveSiteIDs, HostAuthority: authority})
			if err != nil {
				return nil, err
			}
			for _, row := range retained {
				readings = append(readings, sqlc.ListHostedAccountHistoricalReadingsRow(row))
			}
		}
		readingIndex := 0
		for at := start; at.Before(end); at = at.Add(step) {
			assets := map[dashboardKey]int32{}
			for _, interval := range intervals {
				if interval.Start.Before(at.Add(step)) && (interval.End == nil || interval.End.After(at)) {
					assets[dashboardKey{interval.Kind, interval.AssetID}] = interval.Kind
				}
			}
			summary := &pb.MiningSummary{AssetCount: int64(len(assets)), UnknownCount: int64(len(assets))}
			temperatureStatus := &telemetrypb.TemperatureStatusCount{Timestamp: timestamppb.New(at)}
			uptimeStatus := &telemetrypb.UptimeStatusCount{Timestamp: timestamppb.New(at)}
			for _, kind := range assets {
				if kind == int32(pb.MiningAssetKind_MINING_ASSET_KIND_MINER) {
					summary.MinerCount++
				} else {
					summary.ModuleCount++
				}
			}
			if readingIndex < len(readings) && readings[readingIndex].Bucket.Equal(at) {
				row := readings[readingIndex]
				readingIndex++
				temperatureStatus.ColdCount = row.TemperatureColdCount
				temperatureStatus.OkCount = row.TemperatureOkCount
				temperatureStatus.HotCount = row.TemperatureHotCount
				temperatureStatus.CriticalCount = row.TemperatureCriticalCount
				uptimeStatus.HashingCount = row.StatusHashingCount
				uptimeStatus.BrokenCount = row.StatusBrokenCount
				uptimeStatus.NotHashingCount = row.StatusNotHashingCount
				summary.HashrateReportingCount = row.HashrateReportingCount
				summary.PowerReportingCount = row.PowerReportingCount
				summary.TemperatureReportingCount = row.TemperatureReportingCount
				summary.PairedReportingCount = row.PairedReportingCount
				summary.PartialModuleCount = row.PartialModuleCount
				summary.HashingCount = row.HashingCount
				summary.OfflineCount = row.OfflineCount
				summary.UnknownCount -= row.HashingCount + row.OfflineCount
				if row.HashrateReportingCount > 0 {
					summary.HashrateHs = &row.HashrateHs
					summary.UptimePercent = &row.UptimePercent
					summary.UptimeReportingCount = row.HashrateReportingCount
				}
				if row.PowerReportingCount > 0 {
					summary.PowerW = &row.PowerW
				}
				if row.TemperatureReportingCount > 0 {
					summary.TemperatureC = &row.TemperatureC
				}
				if row.Complete && row.PairedReportingCount == summary.AssetCount && row.PairedHashrateHs > 0 {
					value := row.PairedPowerW / row.PairedHashrateHs * 1e12
					summary.EfficiencyJTh = &value
				}
			}
			response.Readings = append(response.Readings, &pb.AccountDashboardReading{Time: timestamppb.New(at), Summary: summary, TemperatureStatus: temperatureStatus, UptimeStatus: uptimeStatus})
		}
		return response, nil
	})
}

func (s *Service) AssetDetail(ctx context.Context, actorID, orgID int64, request *pb.GetAccountAssetDetailRequest) (*pb.GetAccountAssetDetailResponse, error) {
	if request == nil || request.Asset == nil {
		return nil, fleeterror.NewInvalidArgumentError("asset required")
	}
	result, err := s.ListAssets(ctx, actorID, orgID, &pb.ListAccountAssetsRequest{AccountId: request.AccountId, SelectedAssets: []*pb.MiningAssetReference{request.Asset}})
	if err != nil {
		return nil, err
	}
	if len(result.Assets) != 1 || !result.Assets[0].Available || result.Assets[0].OutsidePolicy {
		return nil, fleeterror.NewNotFoundError("asset unavailable in selected account")
	}
	asset := result.Assets[0]
	if asset.HashrateHs == nil || asset.PowerW == nil || asset.PartialCoverage || asset.UnknownCoverage {
		asset.EfficiencyJTh = nil
	}
	return &pb.GetAccountAssetDetailResponse{Asset: asset}, nil
}
