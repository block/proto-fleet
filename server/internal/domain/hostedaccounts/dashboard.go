package hostedaccounts

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// These intervals contain established ownership only, never a projection of
// today's ledger into older telemetry. The same typed key is used throughout.
type dashboardInterval struct {
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
	if !end.After(start) || end.Sub(start) > 366*24*time.Hour || step < time.Minute || step > 24*time.Hour || end.Sub(start)/step > 1500 || request.SiteId != nil && *request.SiteId <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("dashboard range must be at most 366 days and 1500 buckets")
	}
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
		response.HistoryAvailableFrom = timestamppb.New(epoch)
		for _, child := range e.children {
			response.Children = append(response.Children, &pb.AccountMiningBucket{AccountId: child.AccountID, Name: child.Name, Suspended: child.Suspended})
		}
		totals := newMiningAccumulator()
		sites := map[int64]*miningAccumulator{}
		siteNames := map[int64]string{}
		for _, row := range e.rows {
			if !e.dashboardSite(row.SiteID) {
				continue
			}
			if request.OwnerAccountId != nil && row.SubclientID != owner {
				continue
			}
			if sites[row.SiteID] == nil {
				sites[row.SiteID] = newMiningAccumulator()
				siteNames[row.SiteID] = row.SiteName
			}
			asset := e.asset(row)
			sites[row.SiteID].add(asset)
			if request.SiteId == nil || row.SiteID == *request.SiteId {
				totals.add(asset)
			}
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
			interval := dashboardInterval{Kind: period.Kind, AssetID: period.AssetID, Identifier: period.DeviceIdentifier, Start: period.StartedAt, SiteID: period.SiteID}
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
		encoded, err := json.Marshal(intervals)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("encode allocation intervals: %w", err)
		}
		authority, err := e.physicalReadAuthority()
		if err != nil {
			return nil, err
		}
		readings, err := q.ListHostedAccountHistoricalReadings(ctx, sqlc.ListHostedAccountHistoricalReadingsParams{Intervals: encoded, StartTime: start, EndTime: end, BucketSeconds: request.BucketSeconds, SiteFilter: request.GetSiteId(), PolicyRestricted: e.policy.EffectiveRestricted, PolicySites: e.policy.EffectiveSiteIDs, HostAuthority: authority})
		if err != nil {
			return nil, err
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
			response.Readings = append(response.Readings, &pb.AccountDashboardReading{Time: timestamppb.New(at), Summary: summary})
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
