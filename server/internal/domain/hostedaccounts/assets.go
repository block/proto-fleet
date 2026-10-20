package hostedaccounts

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/containerreadings"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Bound account admission and telemetry work together, including lock waits
// and transaction retries, so a report or assignment cannot indefinitely delay
// revocation or other organization administration.
const miningTransactionTimeout = 5 * time.Second

type miningEstate struct {
	account         Account
	capabilities    Capabilities
	policy          Sites
	hostPermissions *authz.EffectivePermissions
	rows            []sqlc.ListHostedAccountMiningAssetsRow
	children        []sqlc.ListHostedAccountAssetChildrenRow
}

type miningAdmission bool

const (
	miningRead  miningAdmission = false
	miningWrite miningAdmission = true
)

// Account reports are a distinct entitlement. They never resolve child-private
// authority. The organization lock keeps reporting scope and policy consistent
// with concurrent account administration and allocation transactions.
func loadMiningEstate(ctx context.Context, q sqlc.Querier, actorID, orgID int64, accountID string, includeRows bool, admission miningAdmission) (miningEstate, error) {
	var estate miningEstate
	if err := RequireRequestAccount(ctx, accountID); err != nil {
		return estate, err
	}
	if admission == miningWrite {
		if _, err := lockActor(ctx, q, orgID, actorID); err != nil {
			return estate, err
		}
	} else if err := lockActorForRead(ctx, q, orgID, actorID); err != nil {
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
	authorizeEstate := authorizeForRead
	if admission == miningWrite {
		authorizeEstate = authorize
	}
	estate.capabilities, err = authorizeEstate(ctx, q, actorID, orgID, path, required)
	if err != nil {
		return estate, err
	}
	estate.policy, err = sitePolicy(ctx, q, orgID, path, false)
	if err != nil {
		return estate, err
	}
	estate.hostPermissions, err = hostReadAuthority(ctx, q, actorID, orgID, estate.account, admission)
	if err != nil {
		return estate, err
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

// Host operators reach clients through root ancestor grants (or legacy creator
// memberships), so their host site authority bounds physical metadata in client
// reports exactly as it does in the host report. Customers have no host scope.
func hostReadAuthority(ctx context.Context, q sqlc.Querier, actorID, orgID int64, account Account, admission miningAdmission) (*authz.EffectivePermissions, error) {
	if account.Depth != 0 {
		host, err := q.HasHostedAccountHostAuthority(ctx, sqlc.HasHostedAccountHostAuthorityParams{UserID: actorID, OrganizationID: orgID})
		if err != nil || !host {
			return nil, err
		}
	}
	loadEffective := authz.LoadEffectiveForShare
	if admission == miningWrite {
		loadEffective = authz.LoadEffectiveForUpdate
	}
	effective, err := loadEffective(ctx, q, actorID, orgID)
	if err != nil {
		return nil, err
	}
	if account.Depth != 0 && !effective.Has(authz.PermAccountManage, authz.ResourceContext{}) {
		return nil, nil
	}
	return effective, nil
}

func (e miningEstate) management() bool { return e.capabilities&(ManageMembers|Delegate) != 0 }
func (e miningEstate) physicalRead(row sqlc.ListHostedAccountMiningAssetsRow) bool {
	if e.hostPermissions == nil {
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
	a.Model = row.Model
	a.Name = row.Name
	a.ContainerId = row.ContainerID
	a.ContainerName = row.ContainerName
	a.SiteId = row.SiteID
	a.SiteName = row.SiteName
	a.State = row.State
	if !row.ObservedAt.IsZero() {
		a.ObservedAt = timestamppb.New(row.ObservedAt)
	}
	if row.HasHashrate && finiteNonnegative(row.HashrateHs) {
		a.HashrateHs = &row.HashrateHs
	}
	if row.HasPower && finiteNonnegative(row.PowerW) {
		a.PowerW = &row.PowerW
	}
	if row.HasTemperature && !math.IsInf(row.TemperatureC, 0) && !math.IsNaN(row.TemperatureC) {
		a.TemperatureC = &row.TemperatureC
	}
	if row.HasEfficiency && finiteNonnegative(row.EfficiencyJTh) {
		a.EfficiencyJTh = &row.EfficiencyJTh
	}
	if pb.MiningAssetKind(row.Kind) == pb.MiningAssetKind_MINING_ASSET_KIND_MODULE {
		var coverage *sdk.ContainerRollup
		if json.Unmarshal(row.Coverage, &coverage) != nil || coverage == nil {
			a.UnknownCoverage = true
		} else {
			a.Coverage = containerreadings.Coverage(coverage)
			for _, c := range []sdk.MetricCoverage{coverage.Hashrate, coverage.Power} {
				if !c.TopologyComplete || c.Expected <= 0 {
					a.UnknownCoverage = true
				}
				if c.Expected > 0 && c.Reporting < c.Expected {
					a.PartialCoverage = true
				}
			}
		}
	}

	if a.HashrateHs == nil || a.PowerW == nil || a.PartialCoverage || a.UnknownCoverage {
		a.EfficiencyJTh = nil
	}
	return a
}
func finiteNonnegative(value float64) bool {
	return value >= 0 && !math.IsInf(value, 0) && !math.IsNaN(value)
}

type miningAccumulator struct {
	summary                 *pb.MiningSummary
	pairedHash, pairedPower float64
	temperatureTotal        float64
	incompleteCoverage      bool
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
		a.incompleteCoverage = true
		if asset.Kind == pb.MiningAssetKind_MINING_ASSET_KIND_MODULE {
			s.PartialModuleCount++
		}
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
	if asset.TemperatureC != nil {
		a.temperatureTotal += *asset.TemperatureC
		s.TemperatureReportingCount++
	}
	if asset.HashrateHs != nil {
		if s.UptimePercent == nil {
			s.UptimePercent = new(float64)
		}
		if *asset.HashrateHs > 0 {
			*s.UptimePercent += 100
		}
		s.UptimeReportingCount++
	}
	if asset.HashrateHs != nil && asset.PowerW != nil {
		s.PairedReportingCount++
		a.pairedHash += *asset.HashrateHs
		a.pairedPower += *asset.PowerW
	}
}
func (a *miningAccumulator) result() *pb.MiningSummary {
	if a.pairedHash > 0 && a.summary.PairedReportingCount == a.summary.AssetCount && !a.incompleteCoverage && a.summary.UnknownModuleCount == 0 {
		value := a.pairedPower / a.pairedHash * 1e12
		a.summary.EfficiencyJTh = &value
	}
	if a.summary.TemperatureReportingCount > 0 {
		value := a.temperatureTotal / float64(a.summary.TemperatureReportingCount)
		a.summary.TemperatureC = &value
	}
	if a.summary.UptimeReportingCount > 0 {
		*a.summary.UptimePercent /= float64(a.summary.UptimeReportingCount)
	}
	return a.summary
}

func (s *Service) MiningSummary(ctx context.Context, actorID, orgID int64, accountID string) (*pb.GetAccountMiningSummaryResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, miningTransactionTimeout)
	defer cancel()

	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (*pb.GetAccountMiningSummaryResponse, error) {
		e, err := loadMiningEstate(ctx, q, actorID, orgID, accountID, true, miningRead)
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
	Model         string
	Site          int64
	Kind          int32
	ID            int64
	Sort          int32
	Descending    bool
	Number        float64
	Text          string
	Null          bool
}

func (s *Service) ListAssets(ctx context.Context, actorID, orgID int64, request *pb.ListAccountAssetsRequest) (*pb.ListAccountAssetsResponse, error) {
	if request.PageSize < 0 || request.PageSize > 100 || len(request.Search) > 1020 || len(request.PageToken) > 8192 {
		return nil, fleeterror.NewInvalidArgumentError("invalid asset list")
	}
	if len(request.SelectedAssets) > 500 || len(request.SelectedAssets) > 0 && (request.PageSize != 0 || request.PageToken != "" || request.Search != "" || request.OwnerAccountId != nil || request.ContainerId != nil || request.Sort != nil || request.Model != nil || request.SiteId != nil) {
		return nil, fleeterror.NewInvalidArgumentError("selected assets cannot be combined with pagination or filters")
	}
	field := commonpb.SortField_SORT_FIELD_NAME
	descending := false
	if request.Sort != nil {
		switch request.Sort.Field {
		case commonpb.SortField_SORT_FIELD_UNSPECIFIED, commonpb.SortField_SORT_FIELD_NAME:
		case commonpb.SortField_SORT_FIELD_MODEL, commonpb.SortField_SORT_FIELD_HASHRATE, commonpb.SortField_SORT_FIELD_TEMPERATURE, commonpb.SortField_SORT_FIELD_POWER, commonpb.SortField_SORT_FIELD_EFFICIENCY:
			field = request.Sort.Field
		case commonpb.SortField_SORT_FIELD_IP_ADDRESS, commonpb.SortField_SORT_FIELD_MAC_ADDRESS, commonpb.SortField_SORT_FIELD_ISSUE_COUNT, commonpb.SortField_SORT_FIELD_FIRMWARE, commonpb.SortField_SORT_FIELD_DEVICE_COUNT, commonpb.SortField_SORT_FIELD_LOCATION, commonpb.SortField_SORT_FIELD_WORKER_NAME:
			return nil, fleeterror.NewInvalidArgumentError("unsupported asset sort field")
		default:
			return nil, fleeterror.NewInvalidArgumentError("unsupported asset sort field")
		}
		switch request.Sort.Direction {
		case commonpb.SortDirection_SORT_DIRECTION_UNSPECIFIED, commonpb.SortDirection_SORT_DIRECTION_ASC:
		case commonpb.SortDirection_SORT_DIRECTION_DESC:
			descending = true
		default:
			return nil, fleeterror.NewInvalidArgumentError("invalid asset sort direction")
		}
	}
	if request.Model != nil && (request.GetModel() == "" || len(request.GetModel()) > 1020) || request.SiteId != nil && request.GetSiteId() <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("invalid asset filter")
	}
	seen := map[[2]int64]bool{}
	for _, asset := range request.SelectedAssets {
		if asset == nil || (asset.Kind != pb.MiningAssetKind_MINING_ASSET_KIND_MINER && asset.Kind != pb.MiningAssetKind_MINING_ASSET_KIND_MODULE) || asset.Id <= 0 || seen[[2]int64{int64(asset.Kind), asset.Id}] {
			return nil, fleeterror.NewInvalidArgumentError("invalid selected assets")
		}
		seen[[2]int64{int64(asset.Kind), asset.Id}] = true
	}
	ctx, cancel := context.WithTimeout(ctx, miningTransactionTimeout)
	defer cancel()

	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (*pb.ListAccountAssetsResponse, error) {
		e, err := loadMiningEstate(ctx, q, actorID, orgID, request.AccountId, false, miningRead)
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
		expected := miningCursor{Account: request.AccountId, Actor: actorID, Org: orgID, Search: request.Search, Owner: request.GetOwnerAccountId(), Container: request.GetContainerId(), Model: request.GetModel(), Site: request.GetSiteId(), Sort: int32(field), Descending: descending}
		cursor := expected
		if request.PageToken != "" {
			encoded, err := base64.RawURLEncoding.DecodeString(request.PageToken)
			if err != nil || json.Unmarshal(encoded, &cursor) != nil {
				return nil, fleeterror.NewInvalidArgumentError("invalid asset page token")
			}
			check := cursor
			check.Kind = 0
			check.ID = 0
			check.Number = 0
			check.Text = ""
			check.Null = false
			if check != expected || (cursor.Kind != int32(pb.MiningAssetKind_MINING_ASSET_KIND_MINER) && cursor.Kind != int32(pb.MiningAssetKind_MINING_ASSET_KIND_MODULE)) || cursor.ID <= 0 {
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

		if len(request.SelectedAssets) > 0 {
			selection, err := json.Marshal(request.SelectedAssets)
			if err != nil {
				return nil, fleeterror.NewInternalErrorf("encode asset lookup: %w", err)
			}
			rows, err := q.ListHostedAccountMiningAssets(ctx, sqlc.ListHostedAccountMiningAssetsParams{OrganizationID: orgID, AccountInternalID: e.account.ID, IsRoot: e.account.Depth == 0, SelectedAssets: selection})
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				if e.physicalRead(row) {
					response.Assets = append(response.Assets, e.asset(row))
				}
			}
			return response, nil
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
		encodedAuthority, err := e.physicalReadAuthority()
		if err != nil {
			return nil, err
		}
		options, err := q.ListHostedAccountAssetFilterOptions(ctx, sqlc.ListHostedAccountAssetFilterOptionsParams{
			OrganizationID: orgID, AccountInternalID: e.account.ID, IsRoot: e.account.Depth == 0,
			PolicyRestricted: e.policy.EffectiveRestricted, PolicySites: e.policy.EffectiveSiteIDs, HostAuthority: encodedAuthority,
		})
		if err != nil {
			return nil, err
		}
		response.FilterOptions = assetFilterOptions(options)
		rows, err := q.ListHostedAccountMiningAssetPage(ctx, sqlc.ListHostedAccountMiningAssetPageParams{
			OrganizationID: orgID, AccountInternalID: e.account.ID, IsRoot: e.account.Depth == 0,
			AfterKind: cursor.Kind, AfterID: cursor.ID, HostAuthority: encodedAuthority,
			SortField: int32(field), Descending: descending, HasCursor: request.PageToken != "",
			CursorNull: cursor.Null, CursorNumber: cursor.Number, CursorText: cursor.Text,
			HasOwnerFilter: request.OwnerAccountId != nil, OwnerInternalID: ownerID,
			ContainerFilter: request.GetContainerId(), ModelFilter: request.GetModel(), SiteFilter: request.GetSiteId(), PolicyRestricted: e.policy.EffectiveRestricted, PolicySites: e.policy.EffectiveSiteIDs,
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
			expected.Number = last.SortNumber
			expected.Text = last.SortText
			expected.Null = last.SortNull
			encoded, _ := json.Marshal(expected)
			response.NextPageToken = base64.RawURLEncoding.EncodeToString(encoded)
		}
		for _, row := range rows {
			response.Assets = append(response.Assets, e.asset(sqlc.ListHostedAccountMiningAssetsRow(row)))
		}
		return response, nil
	})
}

func assetFilterOptions(rows []sqlc.ListHostedAccountAssetFilterOptionsRow) *pb.AccountAssetFilterOptions {
	options := &pb.AccountAssetFilterOptions{}
	models := map[string]bool{}
	sites := map[int64]string{}
	containers := map[int64]string{}
	for _, row := range rows {
		if row.Model != "" {
			models[row.Model] = true
		}
		if row.SiteID > 0 {
			sites[row.SiteID] = row.SiteName
		}
		if row.ContainerID > 0 {
			containers[row.ContainerID] = row.ContainerName
		}
	}
	for model := range models {
		options.Models = append(options.Models, model)
	}
	slices.Sort(options.Models)
	locations := func(values map[int64]string) []*pb.AccountAssetLocationOption {
		result := make([]*pb.AccountAssetLocationOption, 0, len(values))
		for id, name := range values {
			result = append(result, &pb.AccountAssetLocationOption{Id: id, Name: name})
		}
		slices.SortFunc(result, func(a, b *pb.AccountAssetLocationOption) int {
			if order := strings.Compare(a.Name, b.Name); order != 0 {
				return order
			}
			return cmp.Compare(a.Id, b.Id)
		})
		return result
	}
	options.Sites = locations(sites)
	options.Containers = locations(containers)
	return options
}

// AssignAssets validates the exact reviewed snapshot after physical bindings and
// container reconciliation are locked. The allocation ledger is reporting-only.
func (s *Service) AssignAssets(ctx context.Context, actorID, orgID int64, request *pb.AssignAccountAssetsRequest) error {
	if len(request.Assets) == 0 || len(request.Assets) > 500 || (request.Intent != pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN && request.Intent != pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL) {
		return fleeterror.NewInvalidArgumentError("invalid asset assignment")
	}
	// Binding and inventory lock waits run under organization admission. Bound the
	// whole assignment so contention cannot retain that admission indefinitely.
	ctx, cancel := context.WithTimeout(ctx, miningTransactionTimeout)
	defer cancel()

	return db.WithTransactionTimeoutNoResult(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) error {
		e, err := loadMiningEstate(ctx, q, actorID, orgID, request.AccountId, false, miningWrite)
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
		}
		selectedAssets, err := json.Marshal(request.Assets)
		if err != nil {
			return fleeterror.NewInternalErrorf("encode selected mining assets: %w", err)
		}
		loadSelection := func() (map[[2]int64]sqlc.ListHostedAccountMiningAssetsRow, error) {
			rows, err := q.ListHostedAccountMiningAssets(ctx, sqlc.ListHostedAccountMiningAssetsParams{OrganizationID: orgID, AccountInternalID: e.account.ID, IsRoot: e.account.Depth == 0, SelectedAssets: selectedAssets})
			if err != nil {
				return nil, err
			}
			current := map[[2]int64]sqlc.ListHostedAccountMiningAssetsRow{}
			for _, row := range rows {
				current[[2]int64{int64(row.Kind), row.ID}] = row
			}
			for key, selection := range selected {
				row, ok := current[key]
				if !ok || row.Revision != selection.ExpectedRevision || !e.physicalRead(row) {
					return nil, assetConflict()
				}
				if assigning {
					if !row.Available || !siteAllowed(e.policy, row.SiteID) {
						return nil, assetConflict()
					}
					if !siteAllowed(destinationPolicy, row.SiteID) {
						return nil, fleeterror.NewInvalidArgumentError("selected assets are outside the destination account's site policy")
					}
					if e.account.Depth == 0 && row.ClientID != 0 || e.account.Depth == 1 && (row.ClientID != e.account.ID || row.SubclientID != 0) {
						return nil, assetConflict()
					}
				} else if e.account.Depth == 0 && row.ClientID != child.ID || e.account.Depth == 1 && (row.ClientID != e.account.ID || row.SubclientID != child.ID) {
					return nil, assetConflict()
				}
			}
			return current, nil
		}
		// Reject the entire batch before acquiring any physical-device locks.
		// The same checks run again after locking to close placement/retirement races.
		if _, err := loadSelection(); err != nil {
			return err
		}
		for _, selection := range request.Assets {
			deviceID, err := q.GetHostedAccountAssetDevice(ctx, sqlc.GetHostedAccountAssetDeviceParams{OrganizationID: orgID, Kind: int32(selection.Kind), AssetID: selection.Id})
			if err != nil {
				return missingAssetConflict(err)
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
				return missingAssetConflict(err)
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
		current, err := loadSelection()
		if err != nil {
			return err
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

// A vanished row is a stale selection. Deadlines, deadlocks and connection
// failures must keep their cause so transactions retry or report them honestly.
func missingAssetConflict(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return assetConflict()
	}
	return err
}
