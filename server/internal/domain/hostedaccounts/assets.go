package hostedaccounts

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"math"
	"slices"
	"strings"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type miningEstate struct {
	account         Account
	capabilities    Capabilities
	policy          Sites
	hostPermissions *authz.EffectivePermissions
	rows            []sqlc.ListHostedAccountMiningAssetsRow
	children        []sqlc.ListHostedAccountAssetChildrenRow
}

// Account reports are a distinct entitlement. They never resolve child-private
// authority. The organization lock keeps reporting scope and policy consistent
// with concurrent account administration and allocation transactions.
func loadMiningEstate(ctx context.Context, q sqlc.Querier, actorID, orgID int64, accountID string, includeRows bool) (miningEstate, error) {
	var estate miningEstate
	if err := RequireRequestAccount(ctx, accountID); err != nil {
		return estate, err
	}
	if _, err := lockActor(ctx, q, orgID, actorID); err != nil {
		return estate, err
	}
	path, err := loadPath(ctx, q, orgID, accountID)
	if err != nil {
		return estate, err
	}
	estate.account = path[len(path)-1]
	required := View
	if estate.account.Depth == 0 {
		required = Delegate
	}
	estate.capabilities, err = authorize(ctx, q, actorID, orgID, path, required)
	if err != nil {
		return estate, err
	}
	estate.policy, err = sitePolicy(ctx, q, orgID, path, false)
	if err != nil {
		return estate, err
	}
	if estate.account.Depth == 0 {
		estate.hostPermissions, err = authz.LoadEffectiveForUpdate(ctx, q, actorID, orgID)
		if err != nil {
			return estate, err
		}
	}
	if includeRows {
		estate.rows, err = q.ListHostedAccountMiningAssets(ctx, sqlc.ListHostedAccountMiningAssetsParams{OrganizationID: orgID, AccountInternalID: estate.account.ID, IsRoot: estate.account.Depth == 0, SelectedAssets: json.RawMessage("[]")})
		if err != nil {
			return estate, err
		}
	}
	if estate.management() {
		estate.children, err = q.ListHostedAccountAssetChildren(ctx, sqlc.ListHostedAccountAssetChildrenParams{OrganizationID: orgID, ParentID: sql.NullInt64{Int64: estate.account.ID, Valid: true}})
	}
	return estate, err
}
func (e miningEstate) management() bool { return e.capabilities&(ManageMembers|Delegate) != 0 }
func (e miningEstate) physicalRead(row sqlc.ListHostedAccountMiningAssetsRow) bool {
	if e.account.Depth != 0 {
		return true
	}
	var site *int64
	if row.SiteID > 0 {
		site = &row.SiteID
	}
	for _, permission := range []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead} {
		if !e.hostPermissions.Has(permission, authz.ResourceContext{SiteID: site}) {
			return false
		}
	}
	return true
}
func siteAllowed(policy Sites, siteID int64) bool {
	return !policy.EffectiveRestricted || slices.Contains(policy.EffectiveSiteIDs, siteID)
}
func (e miningEstate) owner(row sqlc.ListHostedAccountMiningAssetsRow) string {
	if e.account.Depth == 0 && row.ClientID != 0 {
		for _, child := range e.children {
			if child.ID == row.ClientID {
				return child.AccountID
			}
		}
	}
	if row.OwnerAccountID == "" {
		return e.account.AccountID
	}
	return row.OwnerAccountID
}
func (e miningEstate) asset(row sqlc.ListHostedAccountMiningAssetsRow) *pb.AccountAsset {
	outside := !siteAllowed(e.policy, row.SiteID)
	a := &pb.AccountAsset{Kind: pb.MiningAssetKind(row.Kind), Id: row.ID, Revision: row.Revision, Available: row.Available, OutsidePolicy: outside, State: "unknown"}
	if e.management() {
		a.OwnerAccountId = e.owner(row)
		a.OwnerAccountName = row.OwnerAccountName
		if e.account.Depth == 0 && row.ClientID != 0 {
			for _, child := range e.children {
				if child.ID == row.ClientID {
					a.OwnerAccountName = child.Name
					break
				}
			}
		}
		if row.OwnerAccountID == "" {
			a.OwnerAccountName = e.account.Name
		}
	}
	if !row.Available || outside {
		if pb.MiningAssetKind(row.Kind) == pb.MiningAssetKind_MINING_ASSET_KIND_MODULE {
			a.UnknownCoverage = true
		}
		return a
	}
	a.Name = row.Name
	a.ContainerId = row.ContainerID
	a.ContainerName = row.ContainerName
	a.SiteId = row.SiteID
	a.SiteName = row.SiteName
	a.State = row.State
	if row.ObservedAt.Valid {
		a.ObservedAt = timestamppb.New(row.ObservedAt.Time)
	}
	if row.HasHashrate && finiteNonnegative(row.HashrateHs) {
		a.HashrateHs = &row.HashrateHs
	}
	if row.HasPower && finiteNonnegative(row.PowerW) {
		a.PowerW = &row.PowerW
	}
	if pb.MiningAssetKind(row.Kind) == pb.MiningAssetKind_MINING_ASSET_KIND_MODULE {
		var coverage map[string]struct {
			Reporting int  `json:"reporting"`
			Expected  int  `json:"expected"`
			Complete  bool `json:"topology_complete"`
		}
		if json.Unmarshal(row.Coverage, &coverage) != nil || coverage == nil {
			a.UnknownCoverage = true
		} else {
			for _, key := range []string{"hashrate", "power"} {
				c, ok := coverage[key]
				if !ok || !c.Complete || c.Expected <= 0 {
					a.UnknownCoverage = true
				}
				if c.Expected > 0 && c.Reporting < c.Expected {
					a.PartialCoverage = true
				}
			}
		}
	}
	return a
}
func finiteNonnegative(value float64) bool {
	return value >= 0 && !math.IsInf(value, 0) && !math.IsNaN(value)
}

type miningAccumulator struct {
	summary                 *pb.MiningSummary
	pairedHash, pairedPower float64
}

func newMiningAccumulator() *miningAccumulator {
	return &miningAccumulator{summary: &pb.MiningSummary{}}
}
func (a *miningAccumulator) add(asset *pb.AccountAsset) {
	s := a.summary
	s.AssetCount++
	if asset.Kind == pb.MiningAssetKind_MINING_ASSET_KIND_MINER {
		s.MinerCount++
	} else {
		s.ModuleCount++
	}
	if !asset.Available {
		s.UnavailableCount++
	}
	if asset.OutsidePolicy {
		s.OutsidePolicyCount++
	}
	if asset.PartialCoverage {
		s.PartialModuleCount++
	}
	if asset.UnknownCoverage {
		s.UnknownModuleCount++
	}
	switch asset.State {
	case "hashing":
		s.HashingCount++
	case "attention":
		s.AttentionCount++
	case "offline":
		s.OfflineCount++
	case "sleeping", "isolated":
		s.SleepingCount++
	default:
		s.UnknownCount++
	}
	if asset.HashrateHs != nil {
		if s.HashrateHs == nil {
			s.HashrateHs = new(float64)
		}
		*s.HashrateHs += *asset.HashrateHs
		s.HashrateReportingCount++
	}
	if asset.PowerW != nil {
		if s.PowerW == nil {
			s.PowerW = new(float64)
		}
		*s.PowerW += *asset.PowerW
		s.PowerReportingCount++
	}
	if asset.HashrateHs != nil && asset.PowerW != nil {
		s.PairedReportingCount++
		a.pairedHash += *asset.HashrateHs
		a.pairedPower += *asset.PowerW
	}
}
func (a *miningAccumulator) result() *pb.MiningSummary {
	if a.pairedHash > 0 {
		value := a.pairedPower / a.pairedHash * 1e12
		a.summary.EfficiencyJTh = &value
	}
	return a.summary
}

func (s *Service) MiningSummary(ctx context.Context, actorID, orgID int64, accountID string) (*pb.GetAccountMiningSummaryResponse, error) {
	return db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (*pb.GetAccountMiningSummaryResponse, error) {
		e, err := loadMiningEstate(ctx, q, actorID, orgID, accountID, true)
		if err != nil {
			return nil, err
		}
		total, direct := newMiningAccumulator(), newMiningAccumulator()
		buckets := map[string]*miningAccumulator{}
		response := &pb.GetAccountMiningSummaryResponse{CanManageAssignments: e.capabilities&Delegate != 0 && e.account.Depth < 2, GeneratedAt: timestamppb.New(time.Now())}
		for _, child := range e.children {
			buckets[child.AccountID] = newMiningAccumulator()
			response.Children = append(response.Children, &pb.AccountMiningBucket{AccountId: child.AccountID, Name: child.Name, Suspended: child.Suspended})
		}
		for _, row := range e.rows {
			if !e.physicalRead(row) {
				continue
			}
			asset := e.asset(row)
			total.add(asset)
			owner := e.owner(row)
			if owner == accountID {
				direct.add(asset)
			} else if bucket := buckets[owner]; bucket != nil {
				bucket.add(asset)
			}
		}
		response.Totals = total.result()
		if e.management() {
			response.Direct = direct.result()
		}
		for _, child := range response.Children {
			child.Summary = buckets[child.AccountId].result()
		}
		return response, nil
	})
}

type miningCursor struct {
	Account       string
	Actor, Org    int64
	Search, Owner string
	Container     int64
	Kind          int32
	ID            int64
}

func (s *Service) ListAssets(ctx context.Context, actorID, orgID int64, request *pb.ListAccountAssetsRequest) (*pb.ListAccountAssetsResponse, error) {
	if request.PageSize < 0 || request.PageSize > 100 || len(request.Search) > 1020 {
		return nil, fleeterror.NewInvalidArgumentError("invalid asset list")
	}
	return db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (*pb.ListAccountAssetsResponse, error) {
		e, err := loadMiningEstate(ctx, q, actorID, orgID, request.AccountId, false)
		if err != nil {
			return nil, err
		}
		if request.OwnerAccountId != nil {
			if !e.management() {
				return nil, denied()
			}
			valid := *request.OwnerAccountId == request.AccountId
			for _, child := range e.children {
				valid = valid || child.AccountID == *request.OwnerAccountId
			}
			if !valid {
				return nil, denied()
			}
		}
		expected := miningCursor{Account: request.AccountId, Actor: actorID, Org: orgID, Search: request.Search, Owner: request.GetOwnerAccountId(), Container: request.GetContainerId()}
		cursor := expected
		if request.PageToken != "" {
			encoded, err := base64.RawURLEncoding.DecodeString(request.PageToken)
			if err != nil || json.Unmarshal(encoded, &cursor) != nil {
				return nil, fleeterror.NewInvalidArgumentError("invalid asset page token")
			}
			check := cursor
			check.Kind = 0
			check.ID = 0
			if check != expected {
				return nil, fleeterror.NewInvalidArgumentError("asset page token scope changed")
			}
		}
		limit := request.PageSize
		if limit == 0 {
			limit = 50
		}
		response := &pb.ListAccountAssetsResponse{CanManageAssignments: e.capabilities&Delegate != 0 && e.account.Depth < 2, CanViewAssignments: e.management()}
		for _, child := range e.children {
			response.Children = append(response.Children, &pb.AccountMiningBucket{AccountId: child.AccountID, Name: child.Name, Suspended: child.Suspended})
		}

		ownerID := int64(0)
		if e.account.Depth == 2 {
			ownerID = e.account.ID
		}
		if request.OwnerAccountId != nil && *request.OwnerAccountId != request.AccountId {
			for _, child := range e.children {
				if child.AccountID == *request.OwnerAccountId {
					ownerID = child.ID
					break
				}
			}
		}
		authority := []map[string]any{}
		if e.account.Depth == 0 {
			for _, permission := range []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead} {
				orgWide, sites := e.hostPermissions.SiteScopeFor(permission)
				if sites == nil {
					sites = []int64{}
				}
				authority = append(authority, map[string]any{"org_wide": orgWide, "sites": sites})
			}
		}
		encodedAuthority, _ := json.Marshal(authority)
		rows, err := q.ListHostedAccountMiningAssetPage(ctx, sqlc.ListHostedAccountMiningAssetPageParams{
			OrganizationID: orgID, AccountInternalID: e.account.ID, IsRoot: e.account.Depth == 0,
			AfterKind: cursor.Kind, AfterID: cursor.ID, HostAuthority: encodedAuthority,
			HasOwnerFilter: request.OwnerAccountId != nil, OwnerInternalID: ownerID,
			ContainerFilter: request.GetContainerId(), PolicyRestricted: e.policy.EffectiveRestricted, PolicySites: e.policy.EffectiveSiteIDs,
			Search: strings.TrimSpace(request.Search), PageLimit: limit + 1,
		})
		if err != nil {
			return nil, err
		}
		if len(rows) > int(limit) {
			rows = rows[:limit]
			last := rows[len(rows)-1]
			expected.Kind = last.Kind
			expected.ID = last.ID
			encoded, _ := json.Marshal(expected)
			response.NextPageToken = base64.RawURLEncoding.EncodeToString(encoded)
		}
		for _, row := range rows {
			response.Assets = append(response.Assets, e.asset(sqlc.ListHostedAccountMiningAssetsRow(row)))
		}
		return response, nil
	})
}

// AssignAssets validates the exact reviewed snapshot after physical bindings and
// container reconciliation are locked. The allocation ledger is reporting-only.
func (s *Service) AssignAssets(ctx context.Context, actorID, orgID int64, request *pb.AssignAccountAssetsRequest) error {
	if len(request.Assets) == 0 || len(request.Assets) > 500 || (request.Intent != pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN && request.Intent != pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL) {
		return fleeterror.NewInvalidArgumentError("invalid asset assignment")
	}
	return db.WithTransactionNoResult(ctx, s.conn, func(q sqlc.Querier) error {
		e, err := loadMiningEstate(ctx, q, actorID, orgID, request.AccountId, false)
		if err != nil {
			return err
		}
		if e.capabilities&Delegate == 0 || e.account.Depth >= 2 {
			return denied()
		}
		childPath, err := loadPath(ctx, q, orgID, request.ChildAccountId)
		if err != nil {
			return err
		}
		child := childPath[len(childPath)-1]
		if child.ParentID.Int64 != e.account.ID {
			return denied()
		}
		assigning := request.Intent == pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN
		if assigning {
			for _, a := range childPath {
				if a.Suspended {
					return denied()
				}
			}
		}
		destinationPolicy, err := sitePolicy(ctx, q, orgID, childPath, false)
		if err != nil {
			return err
		}
		selected := map[[2]int64]*pb.MiningAssetSelection{}
		deviceIDs := []int64{}
		keys := []string{}
		identityByDevice := map[int64][]string{}
		for _, selection := range request.Assets {
			if selection == nil || (selection.Kind != pb.MiningAssetKind_MINING_ASSET_KIND_MINER && selection.Kind != pb.MiningAssetKind_MINING_ASSET_KIND_MODULE) || selection.Id <= 0 || selection.ExpectedRevision < 0 {
				return fleeterror.NewInvalidArgumentError("invalid selected asset")
			}
			key := [2]int64{int64(selection.Kind), selection.Id}
			if selected[key] != nil {
				return fleeterror.NewInvalidArgumentError("duplicate selected asset")
			}
			selected[key] = selection
			deviceID, err := q.GetHostedAccountAssetDevice(ctx, sqlc.GetHostedAccountAssetDeviceParams{OrganizationID: orgID, Kind: int32(selection.Kind), AssetID: selection.Id})
			if err != nil {
				return assetConflict()
			}
			deviceIDs = append(deviceIDs, deviceID)
			identity, err := q.GetDeviceWriteIdentityKeys(ctx, deviceID)
			if err != nil {
				return err
			}
			keys = append(keys, identity...)
			identityByDevice[deviceID] = identity
		}
		if err = q.LockDeviceWriteIdentities(ctx, keys); err != nil {
			return err
		}
		slices.Sort(deviceIDs)
		deviceIDs = slices.Compact(deviceIDs)
		for _, deviceID := range deviceIDs {
			discoveryID, err := q.LockMinerBindingDevice(ctx, deviceID)
			if err != nil {
				return assetConflict()
			}
			if _, err = q.LockMinerBindingDiscovery(ctx, discoveryID); err != nil {
				return err
			}
			if _, err = q.LockMinerBindingPairing(ctx, deviceID); err != nil {
				return err
			}
			if _, err = q.LockMinerBindingCredentials(ctx, deviceID); err != nil {
				return err
			}
			if _, err = q.LockMinerBindingRoutes(ctx, deviceID); err != nil {
				return err
			}
		}
		for _, deviceID := range deviceIDs {
			identity, err := q.GetDeviceWriteIdentityKeys(ctx, deviceID)
			if err != nil {
				return err
			}
			if !slices.Equal(identity, identityByDevice[deviceID]) {
				return assetConflict()
			}
		}
		if err = q.LockHostedAccountAssetInventory(ctx, deviceIDs); err != nil {
			return err
		}
		// Refresh after the last lock: retirement, replacement and site movement
		// committed while we waited must invalidate the original selection.
		selectedAssets, err := json.Marshal(request.Assets)
		if err != nil {
			return fleeterror.NewInternalErrorf("encode selected mining assets: %w", err)
		}
		rows, err := q.ListHostedAccountMiningAssets(ctx, sqlc.ListHostedAccountMiningAssetsParams{OrganizationID: orgID, AccountInternalID: e.account.ID, IsRoot: e.account.Depth == 0, SelectedAssets: selectedAssets})
		if err != nil {
			return err
		}
		current := map[[2]int64]sqlc.ListHostedAccountMiningAssetsRow{}
		for _, row := range rows {
			current[[2]int64{int64(row.Kind), row.ID}] = row
		}
		for key, selection := range selected {
			row, ok := current[key]
			if !ok || row.Revision != selection.ExpectedRevision || !e.physicalRead(row) {
				return assetConflict()
			}
			if assigning {
				if !row.Available || !siteAllowed(e.policy, row.SiteID) || !siteAllowed(destinationPolicy, row.SiteID) {
					return assetConflict()
				}
				if e.account.Depth == 0 && row.ClientID != 0 || e.account.Depth == 1 && (row.ClientID != e.account.ID || row.SubclientID != 0) {
					return assetConflict()
				}
			} else if e.account.Depth == 0 && row.ClientID != child.ID || e.account.Depth == 1 && (row.ClientID != e.account.ID || row.SubclientID != child.ID) {
				return assetConflict()
			}
		}
		for _, selection := range request.Assets {
			row := current[[2]int64{int64(selection.Kind), selection.Id}]
			params := sqlc.SetHostedAccountAssetParams{OrganizationID: orgID, Kind: int32(selection.Kind), AssetID: selection.Id, DeviceID: row.DeviceID}
			if pb.MiningAssetKind(row.Kind) == pb.MiningAssetKind_MINING_ASSET_KIND_MINER {
				params.MinerID = sql.NullInt64{Int64: row.ID, Valid: true}
			} else {
				params.ModuleID = sql.NullInt64{Int64: row.ID, Valid: true}
			}
			if e.account.Depth == 0 {
				if assigning {
					params.ClientID = sql.NullInt64{Int64: child.ID, Valid: true}
				}
			} else {
				params.ClientID = sql.NullInt64{Int64: e.account.ID, Valid: true}
				if assigning {
					params.SubclientID = sql.NullInt64{Int64: child.ID, Valid: true}
				}
			}
			if _, err = q.SetHostedAccountAsset(ctx, params); err != nil {
				return err
			}
		}
		actor, err := q.LockActiveHostedAccountUser(ctx, sqlc.LockActiveHostedAccountUserParams{ID: actorID, OrganizationID: orgID})
		if err != nil {
			return err
		}
		return audit(ctx, q, actor, e.account, "account_assets_assigned", map[string]any{"child_account_id": child.AccountID, "intent": request.Intent.String(), "asset_count": len(request.Assets), "assets": request.Assets})
	})
}
func assetConflict() error {
	return fleeterror.NewFailedPreconditionError("selected assets changed or are unavailable; refresh and review the selection")
}
