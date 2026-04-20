package fleetimport

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgconn"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	poolspb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	models "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
)

// isDuplicateError checks if an error indicates a duplicate/unique constraint violation.
func isDuplicateError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == db.PGUniqueViolation {
		return true
	}
	var fleetErr *fleeterror.FleetError
	if errors.As(err, &fleetErr) && fleetErr.GRPCCode == connect.CodeAlreadyExists {
		return true
	}
	return false
}

// PoolCreator abstracts pool creation so the importer can use the pool service
// (with validation and activity logging) instead of the raw store.
type PoolCreator interface {
	CreatePool(ctx context.Context, poolConfig *poolspb.PoolConfig) (*poolspb.Pool, error)
}

// CollectionManager abstracts collection operations so the importer can use the
// collection service (with validation, transactions, and activity logging).
type CollectionManager interface {
	CreateCollection(ctx context.Context, req *collectionpb.CreateCollectionRequest) (*collectionpb.CreateCollectionResponse, error)
	AddDevicesToCollection(ctx context.Context, req *collectionpb.AddDevicesToCollectionRequest) (*collectionpb.AddDevicesToCollectionResponse, error)
	ListCollections(ctx context.Context, req *collectionpb.ListCollectionsRequest) (*collectionpb.ListCollectionsResponse, error)
}

// Importer creates pools, groups, and racks from source-agnostic import data.
// Used by both Foreman import and CSV import.
type Importer struct {
	poolCreator       PoolCreator
	collectionManager CollectionManager
	deviceStore       interfaces.DeviceStore
}

// NewImporter creates a new Importer.
func NewImporter(
	poolCreator PoolCreator,
	collectionManager CollectionManager,
	deviceStore interfaces.DeviceStore,
) *Importer {
	return &Importer{
		poolCreator:       poolCreator,
		collectionManager: collectionManager,
		deviceStore:       deviceStore,
	}
}

// Import creates pools, groups, and racks from the provided data.
// The caller is responsible for filtering data.Miners to the desired subset
// before calling Import — pool/group/rack derivation is based on the miners provided.
func (imp *Importer) Import(ctx context.Context, orgID int64, data *ImportData) *ImportResult {
	result := &ImportResult{}

	// Resolve MAC → Fleet device mapping once for all device-related operations
	macToDevice := imp.resolveMACToDevice(ctx, orgID, data.Miners)

	result.PoolsCreated = imp.createPools(ctx, data.Pools)
	result.GroupsCreated, result.RacksCreated, result.DevicesAssigned = imp.createGroupsAndRacks(ctx, data, macToDevice)
	result.WorkerNamesSet = imp.setWorkerNames(ctx, data, macToDevice)
	result.MinerNamesSet = imp.setMinerNames(ctx, orgID, data, macToDevice)
	return result
}

// resolveMACToDevice resolves Foreman miner MACs to Fleet device identifiers in a single query.
func (imp *Importer) resolveMACToDevice(ctx context.Context, orgID int64, miners []ImportMiner) map[string]*interfaces.PairedDeviceInfo {
	macs := make([]string, 0, len(miners))
	for _, m := range miners {
		if m.MAC != "" {
			macs = append(macs, networking.NormalizeMAC(m.MAC))
		}
	}
	if len(macs) == 0 {
		return nil
	}
	macToDevice, err := imp.deviceStore.GetPairedDevicesByMACAddresses(ctx, macs, orgID)
	if err != nil {
		slog.Warn("failed to batch lookup devices by MAC", "error", err)
		return nil
	}
	return macToDevice
}

func (imp *Importer) createPools(ctx context.Context, pools []ImportPool) int32 {
	type poolKey struct{ url, username string }
	seen := make(map[poolKey]bool)
	var created int32

	for _, pool := range pools {
		normalizedURL := ensureStratumPrefix(pool.URL)
		key := poolKey{normalizedURL, pool.Username}
		if seen[key] {
			continue
		}
		seen[key] = true

		poolConfig := &poolspb.PoolConfig{
			Url:      normalizedURL,
			Username: pool.Username,
			PoolName: pool.Name,
		}

		if _, err := imp.poolCreator.CreatePool(ctx, poolConfig); err != nil {
			if !isDuplicateError(err) {
				slog.Warn("failed to create pool", "url", pool.URL, "error", err)
			}
		} else {
			created++
		}
	}

	return created
}

func (imp *Importer) setWorkerNames(ctx context.Context, data *ImportData, macToDevice map[string]*interfaces.PairedDeviceInfo) int32 {
	var set int32
	for _, m := range data.Miners {
		if m.MAC == "" {
			continue
		}
		normalizedMAC := networking.NormalizeMAC(m.MAC)
		info, ok := macToDevice[normalizedMAC]
		if !ok {
			continue
		}

		// Use Foreman worker name if available, otherwise normalized MAC (matches regular pairing flow)
		workerName := strings.TrimSpace(m.WorkerName)
		if workerName == "" {
			workerName = normalizedMAC
		}
		if err := imp.deviceStore.UpdateWorkerName(ctx, models.DeviceIdentifier(info.DeviceIdentifier), workerName); err != nil {
			slog.Warn("failed to set worker name", "device", info.DeviceIdentifier, "error", err)
		} else {
			set++
		}
	}
	return set
}

func (imp *Importer) setMinerNames(ctx context.Context, orgID int64, data *ImportData, macToDevice map[string]*interfaces.PairedDeviceInfo) int32 {
	names := make(map[string]string)
	for _, m := range data.Miners {
		if m.MAC == "" || m.Name == "" {
			continue
		}
		info, ok := macToDevice[networking.NormalizeMAC(m.MAC)]
		if !ok {
			continue
		}
		names[info.DeviceIdentifier] = m.Name
	}
	if len(names) == 0 {
		return 0
	}

	if err := imp.deviceStore.UpdateDeviceCustomNames(ctx, orgID, names); err != nil {
		slog.Warn("failed to set miner names from Foreman", "error", err)
		return 0
	}
	return int32(len(names)) //nolint:gosec // bounded by device count
}

// collectionKey combines type and name for deduplication.
type collectionKey struct {
	collType collectionpb.CollectionType
	name     string
}

// buildExistingCollectionMap fetches all existing collections and returns a name→ID lookup.
func (imp *Importer) buildExistingCollectionMap(ctx context.Context) map[collectionKey]int64 {
	existing := make(map[collectionKey]int64)
	for _, ct := range []collectionpb.CollectionType{
		collectionpb.CollectionType_COLLECTION_TYPE_GROUP,
		collectionpb.CollectionType_COLLECTION_TYPE_RACK,
	} {
		pageToken := ""
		for {
			resp, err := imp.collectionManager.ListCollections(ctx, &collectionpb.ListCollectionsRequest{
				Type:      ct,
				PageSize:  1000,
				PageToken: pageToken,
			})
			if err != nil {
				slog.Warn("failed to list existing collections", "type", ct, "error", err)
				break
			}
			for _, c := range resp.Collections {
				existing[collectionKey{ct, c.Label}] = c.Id
			}
			if resp.NextPageToken == "" {
				break
			}
			pageToken = resp.NextPageToken
		}
	}
	return existing
}

// deviceSelector builds a DeviceSelector for a list of device identifiers.
func deviceSelector(deviceIDs []string) *commonpb.DeviceSelector {
	if len(deviceIDs) == 0 {
		return nil
	}
	return &commonpb.DeviceSelector{
		SelectionType: &commonpb.DeviceSelector_DeviceList{
			DeviceList: &commonpb.DeviceIdentifierList{
				DeviceIdentifiers: deviceIDs,
			},
		},
	}
}

func (imp *Importer) createGroupsAndRacks(ctx context.Context, data *ImportData, macToDevice map[string]*interfaces.PairedDeviceInfo) (int32, int32, int32) {
	// Pre-fetch existing collections so we can reconcile duplicates
	existingCollections := imp.buildExistingCollectionMap(ctx)

	// Build miner source ID → Fleet device identifier mapping
	minerMapping := make(map[string]string)
	for _, m := range data.Miners {
		if m.MAC == "" {
			continue
		}
		if info, ok := macToDevice[networking.NormalizeMAC(m.MAC)]; ok {
			minerMapping[m.SourceID] = info.DeviceIdentifier
		}
	}

	// Build rack → miners and group → miners mappings
	type rackInfo struct {
		deviceIDs []string
		maxRow    int32
		maxCol    int32
	}
	rackMiners := make(map[string]*rackInfo)
	groupMinerIDs := make(map[string][]string)

	// Build rack→group lookup
	rackToGroup := make(map[string]string)
	for _, r := range data.Racks {
		if r.GroupID != "" {
			rackToGroup[r.SourceID] = r.GroupID
		}
	}

	for _, m := range data.Miners {
		if m.RackID == "" {
			continue
		}
		devID, ok := minerMapping[m.SourceID]
		if !ok {
			continue
		}

		ri, exists := rackMiners[m.RackID]
		if !exists {
			ri = &rackInfo{}
			rackMiners[m.RackID] = ri
		}
		ri.deviceIDs = append(ri.deviceIDs, devID)
		if m.Row > ri.maxRow {
			ri.maxRow = m.Row
		}
		if m.Column > ri.maxCol {
			ri.maxCol = m.Column
		}

		if groupID, ok := rackToGroup[m.RackID]; ok {
			groupMinerIDs[groupID] = append(groupMinerIDs[groupID], devID)
		}
	}

	// Create groups (or reconcile with existing)
	var groupsCreated int32
	var devicesAssigned int32
	for _, g := range data.Groups {
		key := collectionKey{collectionpb.CollectionType_COLLECTION_TYPE_GROUP, g.Name}
		deviceIDs := groupMinerIDs[g.SourceID]

		if existingID, ok := existingCollections[key]; ok {
			// Existing group — just add devices
			if len(deviceIDs) > 0 {
				resp, err := imp.collectionManager.AddDevicesToCollection(ctx, &collectionpb.AddDevicesToCollectionRequest{
					CollectionId:   existingID,
					DeviceSelector: deviceSelector(deviceIDs),
				})
				if err != nil {
					slog.Warn("failed to add devices to group", "group", g.Name, "error", err)
				} else {
					devicesAssigned += resp.AddedCount
				}
			}
		} else {
			// New group — create with devices atomically
			resp, err := imp.collectionManager.CreateCollection(ctx, &collectionpb.CreateCollectionRequest{
				Type:           collectionpb.CollectionType_COLLECTION_TYPE_GROUP,
				Label:          g.Name,
				DeviceSelector: deviceSelector(deviceIDs),
			})
			if err != nil {
				if !isDuplicateError(err) {
					slog.Warn("failed to create group", "name", g.Name, "error", err)
					continue
				}
				// Race: group was created after we built existingCollections — refresh and add devices.
				existingCollections = imp.buildExistingCollectionMap(ctx)
				if existingID, ok := existingCollections[key]; ok && len(deviceIDs) > 0 {
					if addResp, addErr := imp.collectionManager.AddDevicesToCollection(ctx, &collectionpb.AddDevicesToCollectionRequest{
						CollectionId:   existingID,
						DeviceSelector: deviceSelector(deviceIDs),
					}); addErr != nil {
						slog.Warn("failed to add devices to group after duplicate", "group", g.Name, "error", addErr)
					} else {
						devicesAssigned += addResp.AddedCount
					}
				}
				continue
			}
			groupsCreated++
			devicesAssigned += resp.AddedCount
		}
	}

	// Create racks
	var racksCreated int32
	for _, r := range data.Racks {
		rows := r.Rows
		cols := r.Columns
		if ri, ok := rackMiners[r.SourceID]; ok {
			if ri.maxRow+1 > rows {
				rows = ri.maxRow + 1
			}
			if ri.maxCol+1 > cols {
				cols = ri.maxCol + 1
			}
		}
		if rows <= 0 {
			rows = 1
		}
		if cols <= 0 {
			cols = 1
		}

		location := r.Location
		if location == "" {
			location = "Imported"
		}

		key := collectionKey{collectionpb.CollectionType_COLLECTION_TYPE_RACK, r.Name}
		var rackDeviceIDs []string
		if ri, ok := rackMiners[r.SourceID]; ok {
			rackDeviceIDs = ri.deviceIDs
		}

		if existingID, ok := existingCollections[key]; ok {
			// Existing rack — just add devices
			if len(rackDeviceIDs) > 0 {
				resp, err := imp.collectionManager.AddDevicesToCollection(ctx, &collectionpb.AddDevicesToCollectionRequest{
					CollectionId:   existingID,
					DeviceSelector: deviceSelector(rackDeviceIDs),
				})
				if err != nil {
					slog.Warn("failed to add devices to rack", "rack", r.Name, "error", err)
				} else {
					devicesAssigned += resp.AddedCount
				}
			}
		} else {
			// New rack — create with rack info + devices atomically
			resp, err := imp.collectionManager.CreateCollection(ctx, &collectionpb.CreateCollectionRequest{
				Type:  collectionpb.CollectionType_COLLECTION_TYPE_RACK,
				Label: r.Name,
				TypeDetails: &collectionpb.CreateCollectionRequest_RackInfo{
					RackInfo: &collectionpb.RackInfo{
						Rows:        rows,
						Columns:     cols,
						Zone:        location,
						OrderIndex:  collectionpb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT,
						CoolingType: collectionpb.RackCoolingType_RACK_COOLING_TYPE_AIR,
					},
				},
				DeviceSelector: deviceSelector(rackDeviceIDs),
			})
			if err != nil {
				if !isDuplicateError(err) {
					slog.Warn("failed to create rack", "name", r.Name, "error", err)
					continue
				}
				// Race: rack was created after we built existingCollections — refresh and add devices.
				existingCollections = imp.buildExistingCollectionMap(ctx)
				if existingID, ok := existingCollections[key]; ok && len(rackDeviceIDs) > 0 {
					if addResp, addErr := imp.collectionManager.AddDevicesToCollection(ctx, &collectionpb.AddDevicesToCollectionRequest{
						CollectionId:   existingID,
						DeviceSelector: deviceSelector(rackDeviceIDs),
					}); addErr != nil {
						slog.Warn("failed to add devices to rack after duplicate", "rack", r.Name, "error", addErr)
					} else {
						devicesAssigned += addResp.AddedCount
					}
				}
				continue
			}
			racksCreated++
			devicesAssigned += resp.AddedCount
		}
	}

	return groupsCreated, racksCreated, devicesAssigned
}

func ensureStratumPrefix(url string) string {
	if strings.HasPrefix(url, "stratum+") {
		return url
	}
	if strings.HasPrefix(url, "tcp://") {
		return "stratum+" + url
	}
	if strings.HasPrefix(url, "ssl://") {
		return "stratum+" + url
	}
	return "stratum+tcp://" + url
}
