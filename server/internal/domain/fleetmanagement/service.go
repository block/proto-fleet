package fleetmanagement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/deviceresolver"
	diagnosticsmodels "github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetoptions"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	minerInterfaces "github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	mm "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/netutil"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	telemetryModels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"

	capabilitiespb "github.com/block/proto-fleet/server/generated/grpc/capabilities/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	errorsv1 "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	poolspb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// defaultPageSize is the default number of items returned per page when not specified
	defaultPageSize = 50
	// maxPageSize is the maximum number of items that can be returned per page
	maxPageSize = 1000

	// concurrentUnpairLimit bounds the number of parallel Unpair RPCs
	// fired in the background after a delete operation
	concurrentUnpairLimit = 20

	// unpairTimeout is the per-device timeout for best-effort Unpair calls
	unpairTimeout = 5 * time.Second

	// fleetOptionsFetchTimeout bounds the singleflight fetch that hydrates
	// the per-org option cache. The fetch runs on a context detached from
	// any individual caller (so a caller cancellation does not abort the
	// shared work for siblings); this timeout exists only to prevent a
	// stuck DB connection from leaking the goroutine forever. Set well
	// above any plausible scan time on a healthy DB (target p99 < 250ms)
	// so a slow-but-valid query is not artificially capped.
	fleetOptionsFetchTimeout = 60 * time.Second

	refreshMinersMaxDevices = 50
	// Default fallback used only if a telemetry collector returns an invalid
	// timeout. Production collectors derive this from telemetry configuration.
	refreshMinersPerDeviceTimeout = 10 * time.Second
	refreshMinersSnapshotTimeout  = 2 * time.Second
	refreshMinersConcurrencyLimit = 10
)

// bracketIPv6Host wraps bare IPv6 addresses in brackets for use in URLs.
func bracketIPv6Host(host string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

// constructWebViewURL builds a web view URL
//
// Note: The port is intentionally omitted from the URL for display purposes, as web browsers
// will use the default port for the scheme (80 for http, 443 for https). This matches the
// behavior of GetWebViewURL().
func constructWebViewURL(scheme, ipAddress string) string {
	if ipAddress == "" {
		return ""
	}
	// Only http/https are browser-openable. Other (and untrusted, agent-reported)
	// schemes are stored for routing but must not become clickable UI links — a
	// scheme like "javascript:alert(1)//" would otherwise be an XSS vector.
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return fmt.Sprintf("%s://%s", scheme, bracketIPv6Host(ipAddress))
}

// CapabilitiesProvider provides miner capabilities from plugins.
// Implementations should return device-specific capabilities based on the
// manufacturer, model, and type information in the provided Device.
// Returns nil if capabilities cannot be determined for the device.
type CapabilitiesProvider interface {
	GetMinerCapabilitiesForDevice(ctx context.Context, device *pairingpb.Device) *capabilitiespb.MinerCapabilities
}

type WorkerNamePoolReapplyService interface {
	VerifyCredentials(ctx context.Context, userUsername string, userPassword string) error

	ReapplyCurrentPoolsWithWorkerNames(
		ctx context.Context,
		desiredWorkerNamesByDeviceIdentifier map[string]string,
	) (batchIdentifier string, err error)
}

type Service struct {
	deviceStore           interfaces.DeviceStore
	discoveredDeviceStore interfaces.DiscoveredDeviceStore
	telemetry             TelemetryCollector
	minerService          *miner.Service
	capabilitiesProvider  CapabilitiesProvider
	capabilitiesCache     sync.Map
	poolStore             interfaces.PoolStore
	errorStore            interfaces.ErrorStore
	collectionStore       interfaces.CollectionStore
	buildingStore         interfaces.BuildingStore
	workerNamePoolService WorkerNamePoolReapplyService
	deviceResolver        *deviceresolver.Resolver
	activitySvc           *activity.Service

	// optionsCache holds per-org models + firmware version arrays surfaced
	// by ListMinerStateSnapshots. The TTL is a safety net; pairing and
	// fleet management invalidate it at obvious membership-change sites.
	optionsCache  *fleetoptions.Cache
	optionsSingle singleflight.Group

	// backgroundWg tracks in-flight background Unpair goroutines so they can
	// be awaited during graceful shutdown via WaitForPendingUnpairs.
	backgroundWg sync.WaitGroup

	// unpairSem bounds the total number of concurrent Unpair RPCs
	// across all delete operations. Shared at the service level so that multiple
	// concurrent DeleteMiners calls don't exceed the limit.
	unpairSem chan struct{}

	// refreshMinerSem bounds row refresh network fanout across all callers.
	refreshMinerSem chan struct{}
}

func NewService(
	deviceStore interfaces.DeviceStore,
	discoveredDeviceStore interfaces.DiscoveredDeviceStore,
	t TelemetryCollector,
	minerService *miner.Service,
	capabilitiesProvider CapabilitiesProvider,
	poolStore interfaces.PoolStore,
	errorStore interfaces.ErrorStore,
	collectionStore interfaces.CollectionStore,
	buildingStore interfaces.BuildingStore,
	workerNamePoolService WorkerNamePoolReapplyService,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		deviceStore:           deviceStore,
		discoveredDeviceStore: discoveredDeviceStore,
		telemetry:             t,
		minerService:          minerService,
		capabilitiesProvider:  capabilitiesProvider,
		poolStore:             poolStore,
		errorStore:            errorStore,
		collectionStore:       collectionStore,
		buildingStore:         buildingStore,
		workerNamePoolService: workerNamePoolService,
		activitySvc:           activitySvc,
		deviceResolver:        deviceresolver.New(deviceStore),
		optionsCache:          fleetoptions.NewCache(fleetoptions.DefaultTTL, 1024),
		unpairSem:             make(chan struct{}, concurrentUnpairLimit),
		refreshMinerSem:       make(chan struct{}, refreshMinersConcurrencyLimit),
	}
}

// WithOptionsCache wires the per-org options cache used to serve
// ListMinerStateSnapshots option arrays without re-running DISTINCT scans
// on every page request. Pass nil to disable caching (tests).
func (s *Service) WithOptionsCache(cache *fleetoptions.Cache) {
	s.optionsCache = cache
}

func (s *Service) logActivity(ctx context.Context, event activitymodels.Event) {
	if s.activitySvc != nil {
		s.activitySvc.Log(ctx, event)
	}
}

// resolveDeviceSetSiteScope derives the (site_id, multi_site) scope of a
// multi-device fleet event (#538) from the touched identifiers: a single
// shared site is stamped so the event surfaces under /{site}/activity; a
// set spanning sites (or mixing sited + site-less devices) is marked
// multi_site so it stays out of the unassigned bucket. Best-effort — a
// resolution error leaves the event org-scoped (nil/false) rather than
// failing the action's fire-and-forget audit log. DeleteMiners must call
// this BEFORE soft-deleting, since the query excludes deleted devices.
func (s *Service) resolveDeviceSetSiteScope(ctx context.Context, orgID int64, identifiers []string) activitymodels.SiteScope {
	if s.activitySvc == nil {
		// No activity sink — the scope would only feed an event we never
		// write, so skip the query entirely.
		return activitymodels.SiteScope{}
	}
	sites, err := s.deviceStore.GetDistinctDeviceSiteIDs(ctx, orgID, identifiers)
	if err != nil {
		slog.Warn("failed to resolve device-set site scope for activity log", "error", err)
		return activitymodels.SiteScope{}
	}
	return activitymodels.ResolveSiteScope(sites)
}

// WaitForPendingUnpairs blocks until all background Unpair goroutines
// complete or the timeout expires. Call during graceful server shutdown.
func (s *Service) WaitForPendingUnpairs(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		s.backgroundWg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		slog.Warn("timed out waiting for pending Unpair operations during shutdown")
	}
}

// getCachedCapabilities retrieves capabilities from cache or fetches and caches them
func (s *Service) getCachedCapabilities(ctx context.Context, manufacturer, model, driverName string) *capabilitiespb.MinerCapabilities {
	if s.capabilitiesProvider == nil || manufacturer == "" || model == "" {
		return nil
	}

	cacheKey := manufacturer + "|" + model + "|" + driverName

	if cached, found := s.capabilitiesCache.Load(cacheKey); found {
		if capabilities, ok := cached.(*capabilitiespb.MinerCapabilities); ok {
			return capabilities
		}
		return nil
	}

	device := &pairingpb.Device{
		Manufacturer: manufacturer,
		Model:        model,
		DriverName:   driverName,
	}
	capabilities := s.capabilitiesProvider.GetMinerCapabilitiesForDevice(ctx, device)

	if capabilities != nil {
		s.capabilitiesCache.Store(cacheKey, capabilities)
	}

	return capabilities
}

// validatePageSize validates and normalizes the requested page size
func validatePageSize(pageSize int32) int32 {
	if pageSize <= 0 {
		return defaultPageSize
	}
	if pageSize > maxPageSize {
		return maxPageSize
	}
	return pageSize
}

// ListMinerStateSnapshots returns a paginated list of miners with their metadata (no telemetry)
func (s *Service) ListMinerStateSnapshots(ctx context.Context, req *pb.ListMinerStateSnapshotsRequest) (*pb.ListMinerStateSnapshotsResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	sortConfig := parseSortConfig(req.Sort)

	return s.buildSnapshot(ctx, info.OrganizationID, req.PageSize, req.Cursor, req.Filter, sortConfig)
}

func (s *Service) RefreshMiners(ctx context.Context, req *pb.RefreshMinersRequest) (*pb.RefreshMinersResponse, error) {
	deviceIDs, err := normalizeRefreshMinerIDs(req)
	if err != nil {
		return nil, err
	}

	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	refreshCtx, cancel := context.WithTimeout(ctx, refreshMinersRequestTimeout(len(deviceIDs), s.telemetry.RefreshDeviceTimeout()))
	defer cancel()

	resp := &pb.RefreshMinersResponse{
		Snapshots: []*pb.MinerStateSnapshot{},
		Errors:    map[string]string{},
	}

	type refreshResult struct {
		snapshot *pb.MinerStateSnapshot
		id       string
		errMsg   string
	}

	results := make(chan refreshResult, len(deviceIDs))
	var wg sync.WaitGroup

	for _, id := range deviceIDs {
		deviceID := id
		wg.Add(1)
		go func() {
			defer wg.Done()

			select {
			case s.refreshMinerSem <- struct{}{}:
				defer func() { <-s.refreshMinerSem }()
			case <-refreshCtx.Done():
				results <- refreshResult{id: deviceID, errMsg: sanitizeRefreshMinerError(refreshCtx.Err())}
				return
			}

			device, err := s.deviceStore.GetDeviceByDeviceIdentifier(refreshCtx, deviceID, info.OrganizationID)
			if err != nil {
				if fleeterror.IsNotFoundError(err) {
					results <- refreshResult{id: deviceID, errMsg: "not found"}
					return
				}
				results <- refreshResult{id: deviceID, errMsg: sanitizeRefreshMinerError(err)}
				return
			}

			ownedByFleetNode, err := s.deviceStore.IsDeviceOwnedByFleetNode(refreshCtx, deviceID, info.OrganizationID)
			if err != nil {
				results <- refreshResult{id: deviceID, errMsg: sanitizeRefreshMinerError(err)}
				return
			}
			if ownedByFleetNode {
				results <- refreshResult{id: deviceID, errMsg: "fleet-node-owned miners are not supported by row refresh yet"}
				return
			}

			if err := s.telemetry.RefreshDevice(refreshCtx, telemetryModels.Device{
				ID: telemetryModels.DeviceIdentifier(device.DeviceIdentifier),
			}); err != nil {
				results <- refreshResult{id: deviceID, errMsg: sanitizeRefreshMinerError(err)}
				return
			}

			snapshotCtx, cancel := context.WithTimeout(refreshCtx, refreshMinersSnapshotTimeout)
			defer cancel()

			snapshots, err := s.getMinerStateSnapshotsByIDs(snapshotCtx, info.OrganizationID, []string{deviceID})
			if err != nil {
				results <- refreshResult{id: deviceID, errMsg: sanitizeRefreshMinerError(err)}
				return
			}
			if len(snapshots) == 0 {
				results <- refreshResult{id: deviceID, errMsg: "not found"}
				return
			}

			results <- refreshResult{id: deviceID, snapshot: snapshots[0]}
		}()
	}

	wg.Wait()
	close(results)

	for result := range results {
		if result.errMsg != "" {
			resp.Errors[result.id] = result.errMsg
		}
		if result.snapshot != nil {
			resp.Snapshots = append(resp.Snapshots, result.snapshot)
		}
	}

	return resp, nil
}

func (s *Service) RefreshMinerResourceContexts(ctx context.Context, req *pb.RefreshMinersRequest) (map[string]authz.ResourceContext, error) {
	deviceIDs, err := normalizeRefreshMinerIDs(req)
	if err != nil {
		return nil, err
	}

	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	snapshots, err := s.getMinerStateSnapshotsByIDs(ctx, info.OrganizationID, deviceIDs)
	if err != nil {
		return nil, fleeterror.NewInternalError("failed to authorize miner refresh")
	}

	contexts := make(map[string]authz.ResourceContext, len(deviceIDs))
	snapshotDeviceIDs := make(map[string]struct{}, len(snapshots))
	for _, snapshot := range snapshots {
		snapshotDeviceIDs[snapshot.DeviceIdentifier] = struct{}{}
		if snapshot.Placement == nil || snapshot.Placement.Site == nil {
			contexts[snapshot.DeviceIdentifier] = authz.ResourceContext{}
			continue
		}

		siteID := snapshot.Placement.Site.Id
		contexts[snapshot.DeviceIdentifier] = authz.ResourceContext{SiteID: &siteID}
	}

	for _, deviceID := range deviceIDs {
		if _, ok := snapshotDeviceIDs[deviceID]; ok {
			continue
		}

		siteID, err := s.deviceStore.GetDeviceSiteID(ctx, deviceID, info.OrganizationID)
		if err != nil {
			if fleeterror.IsNotFoundError(err) {
				contexts[deviceID] = authz.ResourceContext{}
				continue
			}
			return nil, fleeterror.NewInternalError("failed to authorize miner refresh")
		}
		if siteID == nil {
			contexts[deviceID] = authz.ResourceContext{}
			continue
		}

		resolvedSiteID := *siteID
		contexts[deviceID] = authz.ResourceContext{SiteID: &resolvedSiteID}
	}

	return contexts, nil
}

func normalizeRefreshMinerIDs(req *pb.RefreshMinersRequest) ([]string, error) {
	if len(req.DeviceIds) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("device_ids must contain at least one device identifier")
	}
	if len(req.DeviceIds) > refreshMinersMaxDevices {
		return nil, fleeterror.NewInvalidArgumentErrorf("device_ids must contain at most %d device identifiers", refreshMinersMaxDevices)
	}

	deviceIDs := make([]string, 0, len(req.DeviceIds))
	for _, id := range req.DeviceIds {
		trimmedID := strings.TrimSpace(id)
		if trimmedID == "" {
			return nil, fleeterror.NewInvalidArgumentError("device_ids cannot contain empty device identifiers")
		}
		deviceIDs = append(deviceIDs, trimmedID)
	}

	return deviceIDs, nil
}

func sanitizeRefreshMinerError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "refresh timed out"
	case errors.Is(err, context.Canceled):
		return "refresh cancelled"
	case fleeterror.IsNotFoundError(err):
		return "not found"
	case fleeterror.IsAuthenticationError(err):
		return "authentication required"
	case fleeterror.IsConnectionError(err):
		return "miner is offline"
	default:
		return "refresh failed"
	}
}

func refreshMinersRequestTimeout(deviceCount int, refreshDeviceTimeout time.Duration) time.Duration {
	if refreshDeviceTimeout <= 0 {
		refreshDeviceTimeout = refreshMinersPerDeviceTimeout
	}
	perWaveTimeout := 2*refreshDeviceTimeout + refreshMinersSnapshotTimeout
	if deviceCount <= 0 {
		return perWaveTimeout
	}
	waves := (deviceCount + refreshMinersConcurrencyLimit - 1) / refreshMinersConcurrencyLimit
	return time.Duration(waves) * perWaveTimeout
}

// GetMinerStateCounts returns counts of miners in different states without fetching miner data
func (s *Service) GetMinerStateCounts(ctx context.Context, req *pb.GetMinerStateCountsRequest) (*pb.GetMinerStateCountsResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	filter, err := stateCountsFilter(req)
	if err != nil {
		return nil, err
	}

	// Both the total and the per-state breakdown must share the same scope,
	// otherwise the dashboard FleetHealth bar mixes a scoped breakdown with
	// an org-wide total.
	total, err := s.deviceStore.GetTotalPairedDevices(ctx, info.OrganizationID, filter)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get total count: %v", err)
	}

	stateCounts, err := s.deviceStore.GetMinerStateCounts(ctx, info.OrganizationID, filter)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get state counts: %v", err)
	}

	return &pb.GetMinerStateCountsResponse{
		TotalMiners: int32(total), //nolint:gosec
		StateCounts: stateCounts,
	}, nil
}

// stateCountsFilter builds the site-scope filter for GetMinerStateCounts,
// applying the same validation as the miner-list site filter. Returns nil
// when no site scope is requested (all-sites).
func stateCountsFilter(req *pb.GetMinerStateCountsRequest) (*interfaces.MinerFilter, error) {
	if len(req.SiteIds) == 0 && !req.IncludeUnassigned {
		return nil, nil
	}
	if len(req.SiteIds) > maxFreeFormFilterValues {
		return nil, fleeterror.NewInvalidArgumentErrorf(
			"site_ids exceeds maximum of %d values", maxFreeFormFilterValues)
	}
	for i, id := range req.SiteIds {
		if id <= 0 {
			return nil, fleeterror.NewInvalidArgumentErrorf("site_ids[%d] must be positive", i)
		}
	}
	return &interfaces.MinerFilter{
		SiteIDs:           req.SiteIds,
		IncludeUnassigned: req.IncludeUnassigned,
	}, nil
}

// GetMinerModelGroups returns model groups with counts, optionally filtered by the provided fleet filter.
func (s *Service) GetMinerModelGroups(ctx context.Context, req *pb.GetMinerModelGroupsRequest) (*pb.GetMinerModelGroupsResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	filter, err := parseFilter(ctx, info.OrganizationID, req.Filter, s.buildingStore)
	if err != nil {
		// parseFilter returns FleetError values (InvalidArgument for oversized
		// free-form arrays, Internal for unsupported enum values). Pass through
		// unchanged so InvalidArgument doesn't surface as a 500.
		return nil, err
	}

	groups, err := s.deviceStore.GetMinerModelGroups(ctx, info.OrganizationID, filter)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get miner model groups: %v", err)
	}

	pbGroups := make([]*pb.MinerModelGroup, 0, len(groups))
	for _, g := range groups {
		pbGroups = append(pbGroups, &pb.MinerModelGroup{
			Model:        g.Model,
			Manufacturer: g.Manufacturer,
			Count:        g.Count,
		})
	}

	return &pb.GetMinerModelGroupsResponse{Groups: pbGroups}, nil
}

// buildSnapshot builds a ListMinerStateSnapshotsResponse with metadata and telemetry data.
// This is the shared implementation used by ListMinerStateSnapshots.
func (s *Service) buildSnapshot(
	ctx context.Context,
	orgID int64,
	pageSize int32,
	cursor string,
	filterProto *pb.MinerListFilter,
	sortConfig *interfaces.SortConfig,
) (*pb.ListMinerStateSnapshotsResponse, error) {
	filter, err := parseFilter(ctx, orgID, filterProto, s.buildingStore)
	if err != nil {
		// Pass FleetError through unchanged; see GetMinerModelGroups for rationale.
		return nil, err
	}

	pageSize = validatePageSize(pageSize)

	snapshots, nextCursor, total, err := s.buildSnapshotsFromUnifiedQuery(ctx, orgID, cursor, pageSize, filter, sortConfig)
	if err != nil {
		return nil, err
	}

	// Enrich snapshots with telemetry and collection labels for paired devices
	pairedDeviceIDs := collectPairedDeviceIdentifiers(snapshots)
	s.populateTelemetryData(ctx, snapshots, pairedDeviceIDs)
	s.populateGroupRefs(ctx, orgID, snapshots, pairedDeviceIDs)
	s.populateRackDetails(ctx, orgID, snapshots, pairedDeviceIDs)

	var stateCounts *telemetrypb.MinerStateCounts
	if shouldIncludeStateCounts(filter.PairingStatuses) {
		stateCounts, err = s.deviceStore.GetMinerStateCounts(ctx, orgID, filter)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get state counts: %v", err)
		}
	}

	options, err := s.getCachedFleetOptions(ctx, orgID)
	if err != nil {
		return nil, err
	}

	return &pb.ListMinerStateSnapshotsResponse{
		Miners:           snapshots,
		Cursor:           nextCursor,
		TotalMiners:      int32(total), //nolint:gosec
		TotalStateCounts: stateCounts,
		Models:           options.Models,
		FirmwareVersions: options.FirmwareVersions,
	}, nil
}

func (s *Service) getMinerStateSnapshotsByIDs(ctx context.Context, orgID int64, deviceIDs []string) ([]*pb.MinerStateSnapshot, error) {
	if len(deviceIDs) == 0 {
		return nil, nil
	}

	pageSize := min(len(deviceIDs), math.MaxInt32)

	// #nosec G115 -- Capped to math.MaxInt32 on the line above, safe for int32.
	snapshots, _, _, err := s.buildSnapshotsFromUnifiedQuery(ctx, orgID, "", int32(pageSize), &interfaces.MinerFilter{
		DeviceIdentifiers: deviceIDs,
	}, nil)
	if err != nil {
		return nil, err
	}

	pairedDeviceIDs := collectPairedDeviceIdentifiers(snapshots)
	s.populateTelemetryData(ctx, snapshots, pairedDeviceIDs)
	s.populateGroupRefs(ctx, orgID, snapshots, pairedDeviceIDs)
	s.populateRackDetails(ctx, orgID, snapshots, pairedDeviceIDs)

	return snapshots, nil
}

// getCachedFleetOptions returns the per-org option arrays surfaced by
// ListMinerStateSnapshots. On a cache miss the underlying DISTINCT scans
// are run once and shared across concurrent callers via singleflight.
//
// The shared fetch runs on a context detached from any individual caller
// (context.WithoutCancel + a fixed timeout) so that a cancellation of
// whichever caller raced into singleflight first does not poison the
// result for siblings whose own contexts are still valid. Each caller
// then selects between the shared result and its own ctx independently.
func (s *Service) getCachedFleetOptions(ctx context.Context, orgID int64) (fleetoptions.Options, error) {
	if opts, ok := s.optionsCache.Get(orgID); ok {
		return opts, nil
	}

	ch := s.optionsSingle.DoChan(strconv.FormatInt(orgID, 10), func() (any, error) {
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fleetOptionsFetchTimeout)
		defer cancel()

		// Re-check after acquiring the singleflight slot in case a sibling
		// caller populated the cache while we were waiting.
		if opts, ok := s.optionsCache.Get(orgID); ok {
			return opts, nil
		}

		models, err := s.deviceStore.GetAvailableModels(fetchCtx, orgID)
		if err != nil {
			return fleetoptions.Options{}, fleeterror.NewInternalErrorf("failed to get available models: %v", err)
		}

		firmwares, err := s.deviceStore.GetAvailableFirmwareVersions(fetchCtx, orgID)
		if err != nil {
			return fleetoptions.Options{}, fleeterror.NewInternalErrorf("failed to get available firmware versions: %v", err)
		}

		opts := fleetoptions.Options{Models: models, FirmwareVersions: firmwares}
		s.optionsCache.Put(orgID, opts)
		return opts, nil
	})

	select {
	case res := <-ch:
		if res.Err != nil {
			// Errors come from the inner func above, which already wraps
			// store errors as fleeterror values. Pass through unchanged.
			return fleetoptions.Options{}, res.Err //nolint:wrapcheck
		}
		opts, ok := res.Val.(fleetoptions.Options)
		if !ok {
			return fleetoptions.Options{}, fleeterror.NewInternalErrorf("unexpected type from options singleflight: %T", res.Val)
		}
		return opts, nil
	case <-ctx.Done():
		// This caller gave up; the detached fetch keeps running in the
		// background and will populate the cache for the next request.
		return fleetoptions.Options{}, ctx.Err() //nolint:wrapcheck
	}
}

func (s *Service) buildSnapshotsFromUnifiedQuery(
	ctx context.Context,
	orgID int64,
	cursor string,
	pageSize int32,
	filter *interfaces.MinerFilter,
	sortConfig *interfaces.SortConfig,
) ([]*pb.MinerStateSnapshot, string, int64, error) {
	rows, nextCursor, total, err := s.deviceStore.ListMinerStateSnapshots(ctx, orgID, cursor, pageSize, filter, sortConfig)
	if err != nil {
		return nil, "", 0, err
	}

	snapshots := make([]*pb.MinerStateSnapshot, 0, len(rows))
	for _, row := range rows {
		snapshot := &pb.MinerStateSnapshot{
			DeviceIdentifier: row.DeviceIdentifier,
			DriverName:       row.DriverName,
		}

		if row.SiteID.Valid {
			id := row.SiteID.Int64
			ensureSnapshotPlacement(snapshot).Site = &commonpb.ResourceRef{
				Id:    id,
				Label: row.SiteLabel,
			}
		}
		if row.BuildingID.Valid {
			id := row.BuildingID.Int64
			ensureSnapshotPlacement(snapshot).Building = &commonpb.ResourceRef{
				Id:    id,
				Label: row.BuildingLabel,
			}
		}

		if row.Model.Valid {
			snapshot.Model = row.Model.String
		}
		if row.Manufacturer.Valid {
			snapshot.Manufacturer = row.Manufacturer.String
		}
		if row.FirmwareVersion.Valid {
			snapshot.FirmwareVersion = row.FirmwareVersion.String
		}
		if row.WorkerName.Valid {
			snapshot.WorkerName = row.WorkerName.String
		}

		switch row.PairingStatus {
		case "PAIRED":
			snapshot.PairingStatus = pb.PairingStatus_PAIRING_STATUS_PAIRED
		case "AUTHENTICATION_NEEDED":
			snapshot.PairingStatus = pb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED
		case "DEFAULT_PASSWORD":
			snapshot.PairingStatus = pb.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD
		case "PENDING":
			snapshot.PairingStatus = pb.PairingStatus_PAIRING_STATUS_PENDING
		case "FAILED":
			snapshot.PairingStatus = pb.PairingStatus_PAIRING_STATUS_FAILED
		case "UNPAIRED":
			snapshot.PairingStatus = pb.PairingStatus_PAIRING_STATUS_UNPAIRED
		default:
			snapshot.PairingStatus = pb.PairingStatus_PAIRING_STATUS_UNPAIRED
		}

		isPaired := isPairedLikeStatus(row.PairingStatus)

		snapshot.Name = ComposeDeviceName(row.CustomName.String, snapshot.Manufacturer, snapshot.Model)

		snapshot.IpAddress = row.IpAddress
		snapshot.Url = constructWebViewURL(row.UrlScheme, row.IpAddress)

		if isPaired {
			snapshot.MacAddress = row.MacAddress
			if row.SerialNumber.Valid {
				snapshot.SerialNumber = row.SerialNumber.String
			}
			if row.DeviceStatus.Valid {
				snapshot.DeviceStatus = convertDeviceStatusStringToProto(string(row.DeviceStatus.DeviceStatusEnum))
			}
		} else {
			snapshot.DeviceStatus = pb.DeviceStatus_DEVICE_STATUS_INACTIVE
		}

		capabilities := s.getCachedCapabilities(ctx, snapshot.Manufacturer, snapshot.Model, row.DriverName)
		if capabilities != nil {
			snapshot.Capabilities = capabilities
		}

		snapshots = append(snapshots, snapshot)
	}

	return snapshots, nextCursor, total, nil
}

// Unit conversion constants for telemetry data
const (
	// hashToTeraHashConversion converts H/s to TH/s (1 TH = 10^12 H)
	hashToTeraHashConversion = 1e12
	// wattsToKilowattsConversion converts W to kW
	wattsToKilowattsConversion = 1000.0
	// joulesPerHashToJoulesPerTeraHashMultiplier converts J/H to J/TH
	// Since 1 TH = 1e12 H, efficiency in J/H * 1e12 = efficiency in J/TH
	joulesPerHashToJoulesPerTeraHashMultiplier = 1e12
)

// isPairedLikeStatus reports whether a pairing_status is paired and reporting
// telemetry. DEFAULT_PASSWORD is treated like PAIRED (its telemetry is trusted).
func isPairedLikeStatus(status string) bool {
	return status == pairing.StatusPaired || status == pairing.StatusDefaultPassword
}

func isPairedLikePairingStatus(status pb.PairingStatus) bool {
	return status == pb.PairingStatus_PAIRING_STATUS_PAIRED ||
		status == pb.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD
}

func collectPairedDeviceIdentifiers(snapshots []*pb.MinerStateSnapshot) []string {
	var ids []string
	for _, s := range snapshots {
		if isPairedLikePairingStatus(s.PairingStatus) {
			ids = append(ids, s.DeviceIdentifier)
		}
	}
	return ids
}

// populateTelemetryData fetches telemetry data for paired devices and populates the snapshot fields.
func (s *Service) populateTelemetryData(ctx context.Context, snapshots []*pb.MinerStateSnapshot, pairedIDs []string) {
	if len(pairedIDs) == 0 {
		return
	}

	deviceIDs := make([]mm.DeviceIdentifier, len(pairedIDs))
	for i, id := range pairedIDs {
		deviceIDs[i] = mm.DeviceIdentifier(id)
	}

	telemetryData, err := s.telemetry.GetLatestDeviceMetrics(ctx, deviceIDs)
	if err != nil {
		slog.Warn("failed to fetch telemetry data for snapshots", "error", err)
		return
	}

	// Populate telemetry fields on snapshots
	for _, snapshot := range snapshots {
		metrics, ok := telemetryData[mm.DeviceIdentifier(snapshot.DeviceIdentifier)]
		if !ok {
			continue
		}

		snapshot.Timestamp = timestamppb.New(metrics.Timestamp)

		if metrics.HashrateHS != nil {
			snapshot.Hashrate = []*commonpb.Measurement{
				convertToMeasurement(metrics.HashrateHS, metrics.Timestamp, commonpb.MeasurementUnit_MEASUREMENT_UNIT_TERAHASH_PER_SECOND, hashToTeraHashConversion),
			}
		}

		if metrics.TempC != nil {
			snapshot.Temperature = []*commonpb.Measurement{
				convertToMeasurement(metrics.TempC, metrics.Timestamp, commonpb.MeasurementUnit_MEASUREMENT_UNIT_CELSIUS, 1.0),
			}
		}

		if metrics.PowerW != nil {
			snapshot.PowerUsage = []*commonpb.Measurement{
				convertToMeasurement(metrics.PowerW, metrics.Timestamp, commonpb.MeasurementUnit_MEASUREMENT_UNIT_KILOWATT, wattsToKilowattsConversion),
			}
		}

		if metrics.EfficiencyJH != nil {
			snapshot.Efficiency = []*commonpb.Measurement{
				convertToMeasurementWithMultiplier(metrics.EfficiencyJH, metrics.Timestamp, commonpb.MeasurementUnit_MEASUREMENT_UNIT_JOULES_PER_TERAHASH, joulesPerHashToJoulesPerTeraHashMultiplier),
			}
		}
	}
}

// populateGroupRefs fetches group refs for paired devices and populates snapshot placement.
func (s *Service) populateGroupRefs(ctx context.Context, orgID int64, snapshots []*pb.MinerStateSnapshot, pairedDeviceIDs []string) {
	if len(pairedDeviceIDs) == 0 {
		return
	}

	groupRefs, err := s.collectionStore.GetGroupRefsForDevices(ctx, orgID, pairedDeviceIDs)
	if err != nil {
		slog.Warn("failed to fetch group refs for snapshots", "error", err)
		return
	}

	// Populate group refs on snapshots
	for _, snapshot := range snapshots {
		if refs, ok := groupRefs[snapshot.DeviceIdentifier]; ok {
			placement := ensureSnapshotPlacement(snapshot)
			placement.Groups = make([]*commonpb.ResourceRef, 0, len(refs))
			for _, ref := range refs {
				placement.Groups = append(placement.Groups, &commonpb.ResourceRef{
					Id:    ref.ID,
					Label: ref.Label,
				})
			}
		}
	}
}

// populateRackDetails fetches rack labels and slot positions for paired devices.
func (s *Service) populateRackDetails(ctx context.Context, orgID int64, snapshots []*pb.MinerStateSnapshot, pairedDeviceIDs []string) {
	if len(pairedDeviceIDs) == 0 {
		return
	}

	rackDetails, err := s.collectionStore.GetRackDetailsForDevices(ctx, orgID, pairedDeviceIDs)
	if err != nil {
		slog.Warn("failed to fetch rack details for snapshots", "error", err)
		return
	}

	// Populate rack details on snapshots
	for _, snapshot := range snapshots {
		if details, ok := rackDetails[snapshot.DeviceIdentifier]; ok {
			placement := ensureSnapshotPlacement(snapshot)
			placement.Rack = &commonpb.ResourceRef{
				Id:    details.ID,
				Label: details.Label,
			}
			snapshot.RackPosition = details.Position
			if details.BuildingID != nil {
				placement.Building = &commonpb.ResourceRef{
					Id:    *details.BuildingID,
					Label: details.BuildingLabel,
				}
			}
		}
	}
}

func ensureSnapshotPlacement(snapshot *pb.MinerStateSnapshot) *commonpb.PlacementRefs {
	if snapshot.Placement == nil {
		snapshot.Placement = &commonpb.PlacementRefs{}
	}
	return snapshot.Placement
}

// convertToMeasurement converts a MetricValue to a proto Measurement by dividing by the conversion factor.
func convertToMeasurement(metric *modelsV2.MetricValue, timestamp time.Time, unit commonpb.MeasurementUnit, divisor float64) *commonpb.Measurement {
	return &commonpb.Measurement{
		Timestamp: timestamppb.New(timestamp),
		Value:     metric.Value / divisor,
		Unit:      unit,
	}
}

// convertToMeasurementWithMultiplier converts a MetricValue to a proto Measurement by multiplying by the conversion factor.
func convertToMeasurementWithMultiplier(metric *modelsV2.MetricValue, timestamp time.Time, unit commonpb.MeasurementUnit, multiplier float64) *commonpb.Measurement {
	return &commonpb.Measurement{
		Timestamp: timestamppb.New(timestamp),
		Value:     metric.Value * multiplier,
		Unit:      unit,
	}
}

// shouldIncludeStateCounts determines if state counts should be fetched based on pairing status filter.
// State counts are meaningful for fleet-visible paired-like devices: PAIRED,
// AUTHENTICATION_NEEDED, and DEFAULT_PASSWORD.
// Per proto definition: empty slice means "no filter" (include all), UNSPECIFIED means "all statuses".
func shouldIncludeStateCounts(pairingStatuses []pb.PairingStatus) bool {
	if len(pairingStatuses) == 0 {
		return true
	}
	for _, status := range pairingStatuses {
		switch status {
		case pb.PairingStatus_PAIRING_STATUS_PAIRED,
			pb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
			pb.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
			pb.PairingStatus_PAIRING_STATUS_UNSPECIFIED:
			return true
		case pb.PairingStatus_PAIRING_STATUS_UNPAIRED,
			pb.PairingStatus_PAIRING_STATUS_PENDING,
			pb.PairingStatus_PAIRING_STATUS_FAILED:
			// These statuses don't have telemetry data, skip
		}
	}
	return false
}

func parseFilter(
	ctx context.Context,
	orgID int64,
	pbFilter *pb.MinerListFilter,
	buildingStore interfaces.BuildingStore,
) (*interfaces.MinerFilter, error) {
	filter := &interfaces.MinerFilter{
		PairingStatuses: []pb.PairingStatus{},
	}

	if pbFilter == nil {
		return filter, nil
	}

	if len(pbFilter.PairingStatuses) > 0 {
		filter.PairingStatuses = pbFilter.PairingStatuses
	}

	// Parse error component types - filter for devices that have errors for specific component types
	if len(pbFilter.ErrorComponentTypes) > 0 {
		componentTypes := make([]diagnosticsmodels.ComponentType, 0, len(pbFilter.ErrorComponentTypes))
		for _, ct := range pbFilter.ErrorComponentTypes {
			componentTypes = append(componentTypes, convertErrorComponentType(ct))
		}
		filter.ErrorComponentTypes = componentTypes
	}

	if len(pbFilter.DeviceStatus) > 0 {
		statusFilters := make([]mm.MinerStatus, 0, len(pbFilter.DeviceStatus))
		for _, status := range pbFilter.DeviceStatus {
			switch status {
			case pb.DeviceStatus_DEVICE_STATUS_ONLINE:
				statusFilters = append(statusFilters, mm.MinerStatusActive)
			case pb.DeviceStatus_DEVICE_STATUS_OFFLINE:
				statusFilters = append(statusFilters, mm.MinerStatusOffline)
			case pb.DeviceStatus_DEVICE_STATUS_MAINTENANCE:
				statusFilters = append(statusFilters, mm.MinerStatusMaintenance)
			case pb.DeviceStatus_DEVICE_STATUS_ERROR:
				statusFilters = append(statusFilters, mm.MinerStatusError)
			case pb.DeviceStatus_DEVICE_STATUS_UNSPECIFIED:
				statusFilters = append(statusFilters, mm.MinerStatusUnknown)
			case pb.DeviceStatus_DEVICE_STATUS_INACTIVE:
				statusFilters = append(statusFilters, mm.MinerStatusInactive)
			case pb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL:
				statusFilters = append(statusFilters, mm.MinerStatusNeedsMiningPool)
			case pb.DeviceStatus_DEVICE_STATUS_UPDATING:
				statusFilters = append(statusFilters, mm.MinerStatusUpdating)
			case pb.DeviceStatus_DEVICE_STATUS_REBOOT_REQUIRED:
				statusFilters = append(statusFilters, mm.MinerStatusRebootRequired)
			default:
				return nil, fleeterror.NewInternalErrorf("unsupported miner status: %v", status)
			}
		}
		filter.DeviceStatusFilter = statusFilters
	}

	if len(pbFilter.Models) > 0 {
		filter.ModelNames = pbFilter.Models
	}

	if len(pbFilter.GroupIds) > 0 {
		filter.GroupIDs = pbFilter.GroupIds
	}

	if len(pbFilter.RackIds) > 0 {
		filter.RackIDs = pbFilter.RackIds
	}

	if len(pbFilter.FirmwareVersions) > 0 {
		if len(pbFilter.FirmwareVersions) > maxFreeFormFilterValues {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"firmware_versions exceeds maximum of %d values", maxFreeFormFilterValues)
		}
		filter.FirmwareVersions = pbFilter.FirmwareVersions
	}

	// Legacy `zones` field (deprecated, field 10): translate to wildcard
	// ZoneKeys so older clients keep working. New callers should emit
	// zone_keys directly with explicit building_id. Validated under the
	// same cap and non-empty rule as zone_keys.
	legacyZoneKeys := make([]interfaces.ZoneKey, 0, len(pbFilter.Zones)) //nolint:staticcheck // SA1019 — intentional translation of deprecated field
	if len(pbFilter.Zones) > 0 {                                         //nolint:staticcheck // SA1019 — see comment above
		if len(pbFilter.Zones) > maxFreeFormFilterValues { //nolint:staticcheck // SA1019
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"zones exceeds maximum of %d values", maxFreeFormFilterValues)
		}
		for i, z := range pbFilter.Zones { //nolint:staticcheck // SA1019
			if z == "" {
				return nil, fleeterror.NewInvalidArgumentErrorf(
					"zones[%d] must be non-empty", i)
			}
			legacyZoneKeys = append(legacyZoneKeys, interfaces.ZoneKey{BuildingID: 0, Zone: z})
		}
	}

	if len(pbFilter.BuildingIds) > 0 {
		if len(pbFilter.BuildingIds) > maxFreeFormFilterValues {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"building_ids exceeds maximum of %d values", maxFreeFormFilterValues)
		}
		for i, id := range pbFilter.BuildingIds {
			if id <= 0 {
				return nil, fleeterror.NewInvalidArgumentErrorf(
					"building_ids[%d] must be positive", i)
			}
		}
		filter.BuildingIDs = pbFilter.BuildingIds
	}
	filter.IncludeNoBuilding = pbFilter.IncludeNoBuilding

	if len(pbFilter.ZoneKeys) > 0 {
		if len(pbFilter.ZoneKeys) > maxFreeFormFilterValues {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"zone_keys exceeds maximum of %d values", maxFreeFormFilterValues)
		}
		filter.ZoneKeys = make([]interfaces.ZoneKey, 0, len(pbFilter.ZoneKeys))
		for i, zk := range pbFilter.ZoneKeys {
			if zk == nil {
				return nil, fleeterror.NewInvalidArgumentErrorf(
					"zone_keys[%d] is nil", i)
			}
			if zk.BuildingId < 0 {
				return nil, fleeterror.NewInvalidArgumentErrorf(
					"zone_keys[%d].building_id must be non-negative", i)
			}
			if zk.Zone == "" {
				return nil, fleeterror.NewInvalidArgumentErrorf(
					"zone_keys[%d].zone must be non-empty", i)
			}
			filter.ZoneKeys = append(filter.ZoneKeys, interfaces.ZoneKey{
				BuildingID: zk.BuildingId,
				Zone:       zk.Zone,
			})
		}
	}
	filter.IncludeNoRack = pbFilter.IncludeNoRack

	// Append legacy `zones` translations after zone_keys validation so
	// older clients sending `zones: ["A"]` get the same wildcard match
	// they had pre-#229. New callers should emit zone_keys directly.
	if len(legacyZoneKeys) > 0 {
		filter.ZoneKeys = append(filter.ZoneKeys, legacyZoneKeys...)
	}

	// include_no_rack widens results to devices with no rack membership,
	// but the zone_keys predicate requires a rack membership row — the
	// combination silently drops every unracked device the caller asked
	// to include. Reject explicitly so the contradiction surfaces as
	// InvalidArgument instead of a misleading empty-or-narrowed result.
	if filter.IncludeNoRack && len(filter.ZoneKeys) > 0 {
		return nil, fleeterror.NewInvalidArgumentErrorf(
			"include_no_rack cannot be combined with zone_keys (or legacy zones)")
	}

	// Cross-org check for explicit building IDs (building_ids + scoped
	// zone_keys.building_id > 0). Wildcards (building_id == 0) skip the
	// check — there is no specific building to validate. The SQL
	// builder's org_id predicate is the single-layer defense for the
	// wildcard path; see device_filters_orgid_audit_test.go. Shared
	// helper lives in interfaces/filtervalidation.go so the
	// fleetmanagement and collection paths can't drift.
	if err := interfaces.ValidateFilterBuildings(ctx, orgID, filter.BuildingIDs, filter.ZoneKeys, buildingStore); err != nil {
		return nil, err
	}

	if len(pbFilter.NumericRanges) > 0 {
		if len(pbFilter.NumericRanges) > maxFreeFormFilterValues {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"numeric_ranges exceeds maximum of %d values", maxFreeFormFilterValues)
		}
		ranges := make([]interfaces.NumericRange, 0, len(pbFilter.NumericRanges))
		for i, r := range pbFilter.NumericRanges {
			parsed, err := parseNumericRange(i, r)
			if err != nil {
				return nil, err
			}
			ranges = append(ranges, parsed)
		}
		filter.NumericRanges = ranges
	}

	if len(pbFilter.IpCidrs) > 0 {
		if len(pbFilter.IpCidrs) > maxFreeFormFilterValues {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"ip_cidrs exceeds maximum of %d values", maxFreeFormFilterValues)
		}
		prefixes := make([]netip.Prefix, 0, len(pbFilter.IpCidrs))
		for i, c := range pbFilter.IpCidrs {
			p, err := parseCIDR(i, c)
			if err != nil {
				return nil, err
			}
			prefixes = append(prefixes, p)
		}
		filter.IPCIDRs = prefixes
	}

	if len(pbFilter.SiteIds) > 0 {
		if len(pbFilter.SiteIds) > maxFreeFormFilterValues {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"site_ids exceeds maximum of %d values", maxFreeFormFilterValues)
		}
		for i, id := range pbFilter.SiteIds {
			if id <= 0 {
				return nil, fleeterror.NewInvalidArgumentErrorf(
					"site_ids[%d] must be positive", i)
			}
		}
		filter.SiteIDs = pbFilter.SiteIds
	}
	filter.IncludeUnassigned = pbFilter.IncludeUnassigned

	return filter, nil
}

func parseNumericRange(idx int, r *pb.NumericRangeFilter) (interfaces.NumericRange, error) {
	if r == nil {
		return interfaces.NumericRange{}, fleeterror.NewInvalidArgumentErrorf(
			"numeric_ranges[%d] is nil", idx)
	}
	field, err := convertNumericField(r.Field)
	if err != nil {
		return interfaces.NumericRange{}, fleeterror.NewInvalidArgumentErrorf(
			"numeric_ranges[%d].field: %v", idx, err)
	}
	if r.Min == nil && r.Max == nil {
		return interfaces.NumericRange{}, fleeterror.NewInvalidArgumentErrorf(
			"numeric_ranges[%d]: at least one of min or max must be set", idx)
	}
	out := interfaces.NumericRange{
		Field:        field,
		MinInclusive: r.MinInclusive,
		MaxInclusive: r.MaxInclusive,
	}
	if r.Min != nil {
		v := r.Min.Value
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return interfaces.NumericRange{}, fleeterror.NewInvalidArgumentErrorf(
				"numeric_ranges[%d].min must be finite", idx)
		}
		out.Min = &v
	}
	if r.Max != nil {
		v := r.Max.Value
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return interfaces.NumericRange{}, fleeterror.NewInvalidArgumentErrorf(
				"numeric_ranges[%d].max must be finite", idx)
		}
		out.Max = &v
	}
	if out.Min != nil && out.Max != nil && *out.Min > *out.Max {
		return interfaces.NumericRange{}, fleeterror.NewInvalidArgumentErrorf(
			"numeric_ranges[%d]: min (%v) must not exceed max (%v)", idx, *out.Min, *out.Max)
	}
	return out, nil
}

func convertNumericField(f pb.NumericField) (interfaces.NumericFilterField, error) {
	switch f {
	case pb.NumericField_NUMERIC_FIELD_HASHRATE_THS:
		return interfaces.NumericFilterFieldHashrateTHs, nil
	case pb.NumericField_NUMERIC_FIELD_EFFICIENCY_JTH:
		return interfaces.NumericFilterFieldEfficiencyJTH, nil
	case pb.NumericField_NUMERIC_FIELD_POWER_KW:
		return interfaces.NumericFilterFieldPowerKW, nil
	case pb.NumericField_NUMERIC_FIELD_TEMPERATURE_C:
		return interfaces.NumericFilterFieldTemperatureC, nil
	case pb.NumericField_NUMERIC_FIELD_VOLTAGE_V:
		return interfaces.NumericFilterFieldVoltageV, nil
	case pb.NumericField_NUMERIC_FIELD_CURRENT_A:
		return interfaces.NumericFilterFieldCurrentA, nil
	case pb.NumericField_NUMERIC_FIELD_UNSPECIFIED:
		return 0, fmt.Errorf("field is required")
	}
	return 0, fmt.Errorf("unsupported field %v", f)
}

// parseCIDR wraps netutil.ParseCIDROrIP with the ip_cidrs[idx] error
// context expected by FleetManagement filter callers.
func parseCIDR(idx int, raw string) (netip.Prefix, error) {
	prefix, err := netutil.ParseCIDROrIP(raw)
	if err != nil {
		return netip.Prefix{}, fleeterror.NewInvalidArgumentErrorf(
			"ip_cidrs[%d]: %v", idx, err)
	}
	return prefix, nil
}

// maxFreeFormFilterValues caps the size of free-form repeated-string filter
// arrays (firmware_versions, zones). Real fleets have a handful of distinct
// firmware versions or zones; arbitrarily large arrays from a misbehaving or
// hostile client would balloon Postgres planner cost on `= ANY($N::text[])`.
const maxFreeFormFilterValues = 1024

// convertErrorComponentType converts a proto ComponentType to domain ComponentType.
func convertErrorComponentType(ct errorsv1.ComponentType) diagnosticsmodels.ComponentType {
	switch ct {
	case errorsv1.ComponentType_COMPONENT_TYPE_PSU:
		return diagnosticsmodels.ComponentTypePSU
	case errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD:
		return diagnosticsmodels.ComponentTypeHashBoards
	case errorsv1.ComponentType_COMPONENT_TYPE_FAN:
		return diagnosticsmodels.ComponentTypeFans
	case errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD:
		return diagnosticsmodels.ComponentTypeControlBoard
	case errorsv1.ComponentType_COMPONENT_TYPE_UNSPECIFIED,
		errorsv1.ComponentType_COMPONENT_TYPE_EEPROM,
		errorsv1.ComponentType_COMPONENT_TYPE_IO_MODULE:
		return diagnosticsmodels.ComponentTypeUnspecified
	}
	return diagnosticsmodels.ComponentTypeUnspecified
}

// convertDeviceStatusStringToProto converts a database device status string to proto enum
func convertDeviceStatusStringToProto(status string) pb.DeviceStatus {
	switch strings.ToUpper(status) {
	case "ACTIVE":
		return pb.DeviceStatus_DEVICE_STATUS_ONLINE
	case "OFFLINE":
		return pb.DeviceStatus_DEVICE_STATUS_OFFLINE
	case "MAINTENANCE":
		return pb.DeviceStatus_DEVICE_STATUS_MAINTENANCE
	case "ERROR":
		return pb.DeviceStatus_DEVICE_STATUS_ERROR
	case "INACTIVE":
		return pb.DeviceStatus_DEVICE_STATUS_INACTIVE
	case "NEEDS_MINING_POOL":
		return pb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL
	case "UPDATING":
		return pb.DeviceStatus_DEVICE_STATUS_UPDATING
	case "REBOOT_REQUIRED":
		return pb.DeviceStatus_DEVICE_STATUS_REBOOT_REQUIRED
	default:
		return pb.DeviceStatus_DEVICE_STATUS_UNSPECIFIED
	}
}

// GetMinerPoolAssignments retrieves the currently configured pools from a miner
// and matches them with fleet pool definitions to return pool IDs
func (s *Service) GetMinerPoolAssignments(ctx context.Context, req *pb.GetMinerPoolAssignmentsRequest) (*pb.GetMinerPoolAssignmentsResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	// Get the miner by device identifier
	minerDevice, err := s.minerService.GetMinerFromDeviceIdentifier(ctx, mm.DeviceIdentifier(req.DeviceIdentifier))
	if err != nil {
		if isMinerNotFoundError(err) {
			return nil, fleeterror.NewNotFoundErrorf("miner not found: %s", req.DeviceIdentifier)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get miner: %v", err)
	}

	// Verify the miner belongs to the user's organization
	if minerDevice.GetOrgID() != info.OrganizationID {
		return nil, fleeterror.NewNotFoundErrorf("miner not found: %s", req.DeviceIdentifier)
	}

	// Get currently configured pools from the miner
	configuredPools, err := minerDevice.GetMiningPools(ctx)
	if err != nil {
		slog.Error("failed to get mining pools from miner", "deviceID", req.DeviceIdentifier, "error", err)
		return nil, fleeterror.NewInternalErrorf("failed to get mining pools from miner: %v", err)
	}

	// If no pools configured, return empty response
	if len(configuredPools) == 0 {
		return &pb.GetMinerPoolAssignmentsResponse{}, nil
	}

	// Get all fleet pools for matching
	fleetPools, err := s.poolStore.ListPools(ctx, info.OrganizationID)
	if err != nil {
		slog.Error("failed to list fleet pools", "orgID", info.OrganizationID, "error", err)
		return nil, fleeterror.NewInternalErrorf("failed to list fleet pools: %v", err)
	}

	// Sort pools by priority to ensure consistent ordering
	// (miner API does not guarantee order)
	sort.Slice(configuredPools, func(i, j int) bool {
		return configuredPools[i].Priority < configuredPools[j].Priority
	})

	pools := make([]*pb.PoolAssignment, 0, len(configuredPools))
	for _, configuredPool := range configuredPools {
		assignment := &pb.PoolAssignment{
			Url:      configuredPool.URL,
			Username: configuredPool.Username,
			PoolId:   findMatchingFleetPoolID(configuredPool.URL, configuredPool.Username, fleetPools),
		}
		pools = append(pools, assignment)
	}

	return &pb.GetMinerPoolAssignmentsResponse{Pools: pools}, nil
}

// GetMinerCoolingMode retrieves the currently configured cooling mode from a miner.
func (s *Service) GetMinerCoolingMode(ctx context.Context, req *pb.GetMinerCoolingModeRequest) (*pb.GetMinerCoolingModeResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	// Get the miner by device identifier
	minerDevice, err := s.minerService.GetMinerFromDeviceIdentifier(ctx, mm.DeviceIdentifier(req.DeviceIdentifier))
	if err != nil {
		if isMinerNotFoundError(err) {
			return nil, fleeterror.NewNotFoundErrorf("miner not found: %s", req.DeviceIdentifier)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get miner: %v", err)
	}

	// Verify the miner belongs to the user's organization
	if minerDevice.GetOrgID() != info.OrganizationID {
		return nil, fleeterror.NewNotFoundErrorf("miner not found: %s", req.DeviceIdentifier)
	}

	// Get current cooling mode from the miner
	coolingMode, err := minerDevice.GetCoolingMode(ctx)
	if err != nil {
		slog.Error("failed to get cooling mode from miner", "deviceID", req.DeviceIdentifier, "error", err)
		return nil, fleeterror.NewInternalErrorf("failed to get cooling mode from miner: %v", err)
	}

	return &pb.GetMinerCoolingModeResponse{CoolingMode: coolingMode}, nil
}

// findMatchingFleetPoolID finds a fleet pool that matches the given URL and username.
// Username matching tries the exact stored username first, then falls back to the
// base username before the first dot so normalized Fleet pools still match miner
// usernames that include an appended worker suffix.
func findMatchingFleetPoolID(url, username string, fleetPools []*poolspb.Pool) *int64 {
	for _, candidate := range poolUsernameMatchCandidates(username) {
		for _, pool := range fleetPools {
			if pool.Url == url && candidate == pool.Username {
				return &pool.PoolId
			}
		}
	}
	return nil
}

func poolUsernameMatchCandidates(username string) []string {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" {
		return []string{""}
	}

	firstSeparator := strings.Index(trimmed, ".")
	if firstSeparator < 0 {
		return []string{trimmed}
	}

	baseUsername := strings.TrimSpace(trimmed[:firstSeparator])
	if baseUsername == trimmed {
		return []string{trimmed}
	}

	return []string{trimmed, baseUsername}
}

// DeleteMiners soft-deletes devices from the fleet and attempts best-effort Unpair on Proto devices.
// The DB deletion always succeeds immediately. Unpair runs in background goroutines and
// failures are logged but never surfaced to the caller.
func (s *Service) DeleteMiners(ctx context.Context, req *pb.DeleteMinersRequest) (*pb.DeleteMinersResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	deviceIdentifiers, err := s.ResolveDeviceIdentifiers(ctx, req.DeviceSelector, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	if len(deviceIdentifiers) == 0 {
		return &pb.DeleteMinersResponse{DeletedCount: 0}, nil
	}

	// Collect Proto miner objects BEFORE soft-delete (lookups filter deleted_at IS NULL)
	miners := s.collectProtoMinersForUnpair(ctx, deviceIdentifiers)

	// Resolve the site scope BEFORE the soft-delete — GetDistinctDeviceSiteIDs
	// filters deleted_at IS NULL, so post-delete it would return nothing and
	// the audit row would fall into the unassigned bucket (#538).
	siteScope := s.resolveDeviceSetSiteScope(ctx, info.OrganizationID, deviceIdentifiers)

	// SoftDeleteDevices verifies ownership and deletes in a single transaction
	// to prevent TOCTOU races between the check and the delete.
	deletedCount, err := s.deviceStore.SoftDeleteDevices(ctx, deviceIdentifiers, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	s.optionsCache.Invalidate(info.OrganizationID)
	for _, id := range deviceIdentifiers {
		s.minerService.InvalidateMiner(mm.DeviceIdentifier(id))
	}

	if err := s.telemetry.RemoveDevices(ctx, telemetryModels.ToDeviceIdentifiers(deviceIdentifiers)...); err != nil {
		slog.Warn("failed to remove devices from telemetry scheduler", "error", err)
	}

	count := int(deletedCount)
	unpairEvent := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           "unpair_miners",
		Description:    "Unpaired miners",
		ScopeCount:     &count,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	}
	unpairEvent.ApplySiteScope(siteScope)
	s.logActivity(ctx, unpairEvent)

	// Best-effort background Unpair for Proto rigs using a bounded worker pool.
	// Workers are tracked by s.backgroundWg so the server can await completion
	// during graceful shutdown via WaitForPendingUnpairs.
	// The shared semaphore limits total concurrent RPCs across all delete calls.
	if len(miners) > 0 {
		minerCh := make(chan minerInterfaces.Miner, len(miners))
		for _, m := range miners {
			minerCh <- m
		}
		close(minerCh)

		numWorkers := min(len(miners), concurrentUnpairLimit)
		for range numWorkers {
			s.backgroundWg.Add(1)
			go func() {
				defer s.backgroundWg.Done()
				for miner := range minerCh {
					s.unpairSem <- struct{}{}

					clearCtx, cancel := context.WithTimeout(context.Background(), unpairTimeout)
					err := miner.Unpair(clearCtx)
					cancel()
					if err != nil {
						slog.Warn("best-effort Unpair failed", "deviceID", miner.GetID(), "error", err)
					}

					<-s.unpairSem
				}
			}()
		}
	}

	cappedCount := min(deletedCount, math.MaxInt32)

	// #nosec G115 -- Capped to math.MaxInt32 on the line above, safe for int32
	return &pb.DeleteMinersResponse{DeletedCount: int32(cappedCount)}, nil
}

// ResolveDeviceIdentifiers resolves a fleetmanagement DeviceSelector (with rich filter support)
// into a list of device identifiers.
func (s *Service) ResolveDeviceIdentifiers(ctx context.Context, selector *pb.DeviceSelector, orgID int64) ([]string, error) {
	if selector == nil {
		return nil, fleeterror.NewInvalidArgumentError("device_selector is required")
	}

	switch sel := selector.SelectionType.(type) {
	case *pb.DeviceSelector_IncludeDevices:
		return s.deviceResolver.ResolveExplicitDevices(ctx, sel.IncludeDevices, orgID)

	case *pb.DeviceSelector_AllDevices:
		filter, err := parseFilter(ctx, orgID, sel.AllDevices, s.buildingStore)
		if err != nil {
			return nil, err
		}
		return s.deviceStore.GetDeviceIdentifiersByOrgWithFilter(ctx, orgID, filter)

	default:
		return nil, fleeterror.NewInvalidArgumentError("device_selector must specify a selection_type")
	}
}

// collectProtoMinersForUnpair collects Miner objects only for Proto rigs.
// Per the RFC, Unpair is only attempted for Proto devices; 3rd-party miners
// (Antminer, etc.) require no device communication on delete.
func (s *Service) collectProtoMinersForUnpair(ctx context.Context, deviceIdentifiers []string) []minerInterfaces.Miner {
	var miners []minerInterfaces.Miner
	for _, id := range deviceIdentifiers {
		m, err := s.minerService.GetMinerFromDeviceIdentifier(ctx, mm.DeviceIdentifier(id))
		if err != nil {
			slog.Debug("skipping Unpair for device", "deviceID", id, "error", err)
			continue
		}
		if m.GetDriverName() != mm.DriverNameProto {
			continue
		}
		miners = append(miners, m)
	}
	return miners
}

// isMinerNotFoundError checks if an error from the miner service indicates the device was not found.
func isMinerNotFoundError(err error) bool {
	return fleeterror.IsNotFoundError(err)
}
