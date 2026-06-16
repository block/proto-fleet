package miner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	lru "github.com/hashicorp/golang-lru/v2/expirable"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	"github.com/block/proto-fleet/server/internal/domain/token"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	// minerCacheTTL is the duration a cached miner handle is considered valid.
	// A short TTL self-heals stale connection coordinates (e.g., after a device
	// moves to a new IP) and credential rotations without requiring explicit
	// invalidation logic for every possible change path. Auth errors and
	// lifecycle events (unpair, delete, password change) still trigger immediate
	// eviction for faster recovery.
	minerCacheTTL = 1 * time.Minute

	// minerCacheSize is the maximum number of miner handles to cache.
	// Sized to cover very large fleets without meaningful memory overhead.
	minerCacheSize = 10_000
)

type minerResolutionMode string

const (
	minerResolutionPairedOnly minerResolutionMode = "paired"
	minerResolutionPairedLike minerResolutionMode = "paired-like"
)

var _ telemetry.CachedMinerGetter = &Service{}

type Service struct {
	// TODO: Refactor this to use a store instead of SQLConnectionManager directly
	sqlstores.SQLConnectionManager
	userStore      stores.UserStore
	encryptService *encrypt.Service
	filesService   *files.Service
	tokenService   *token.Service
	pluginManager  PluginManager

	// commandSender, when set, routes commands for fleet-node-paired devices over the
	// ControlStream; nil disables routing (every device resolves to a direct PluginMiner).
	commandSender remotenode.CommandSender
	// nodeLimiter paces commands per fleet node so a large batch can't oversubscribe
	// a node. Shared across all remote-node miners (keyed by fleet_node id).
	nodeLimiter remotenode.Gate

	// cache stores miner handles keyed by DeviceIdentifier (string).
	// Both GetMiner and GetMinerFromDeviceIdentifier read from and write to
	// this single cache, keeping invalidation simple.
	cache *lru.LRU[string, interfaces.Miner]
}

// WithCommandSender enables fleet-node command routing: a device paired to a CONFIRMED
// fleet node resolves to a remote-node Miner that dispatches over the ControlStream.
func (s *Service) WithCommandSender(sender remotenode.CommandSender) *Service {
	s.commandSender = sender
	s.nodeLimiter = remotenode.NewPerNodeLimiter(remotenode.DefaultPerNodeCommandLimit)
	return s
}

// PluginManager defines the interface for plugin manager operations needed by MinerService
type PluginManager interface {
	HasPluginForDriverName(driverName string) bool
	GetCapabilitiesForDriverName(driverName string) sdk.Capabilities
	plugins.PluginDriverGetter
}

func NewMinerService(db *sql.DB, userStore stores.UserStore, encryptService *encrypt.Service, filesService *files.Service, tokenService *token.Service, pluginManager PluginManager) *Service {
	if db == nil {
		panic("database cannot be nil")
	}
	if encryptService == nil {
		panic("encrypt service cannot be nil")
	}
	if filesService == nil {
		panic("files service cannot be nil")
	}
	if pluginManager == nil {
		panic("plugin manager cannot be nil")
	}

	return &Service{
		SQLConnectionManager: sqlstores.NewSQLConnectionManager(db),
		userStore:            userStore,
		encryptService:       encryptService,
		filesService:         filesService,
		tokenService:         tokenService,
		pluginManager:        pluginManager,
		cache:                lru.NewLRU[string, interfaces.Miner](minerCacheSize, nil, minerCacheTTL),
	}
}

// GetMiner returns the miner handle for the given numeric device ID.
// It performs a lightweight identifier lookup then delegates to
// GetMinerFromDeviceIdentifier so both lookup paths share the same cache.
func (s *Service) GetMiner(ctx context.Context, deviceID int64) (interfaces.Miner, error) {
	return s.getMinerByDatabaseID(ctx, deviceID, minerResolutionPairedLike)
}

func (s *Service) GetMinerForCredentialRemediation(ctx context.Context, deviceID int64) (interfaces.Miner, error) {
	return s.getMinerByDatabaseID(ctx, deviceID, minerResolutionPairedLike)
}

func (s *Service) getMinerByDatabaseID(ctx context.Context, deviceID int64, mode minerResolutionMode) (interfaces.Miner, error) {
	identifier, err := s.GetQueries(ctx).GetDeviceIdentifierByID(ctx, deviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("device not found: %d", deviceID)
		}
		return nil, fmt.Errorf("failed to get device identifier: %w", err)
	}
	return s.getMinerFromDeviceIdentifier(ctx, models.DeviceIdentifier(identifier), mode)
}

func (s *Service) GetMinerFromDeviceIdentifier(ctx context.Context, deviceID models.DeviceIdentifier) (interfaces.Miner, error) {
	return s.getMinerFromDeviceIdentifier(ctx, deviceID, minerResolutionPairedLike)
}

func (s *Service) GetMinerForTelemetry(ctx context.Context, deviceID models.DeviceIdentifier) (interfaces.Miner, error) {
	return s.getMinerFromDeviceIdentifier(ctx, deviceID, minerResolutionPairedLike)
}

func (s *Service) getMinerFromDeviceIdentifier(ctx context.Context, deviceID models.DeviceIdentifier, mode minerResolutionMode) (interfaces.Miner, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("device ID cannot be empty")
	}

	cacheKey := minerCacheKey(mode, deviceID)
	if m, ok := s.cache.Get(cacheKey); ok {
		return m, nil
	}

	// Fleet-node-paired devices route over the ControlStream; check first, cloud-dialed
	// fall through. Deliberately NOT cached: tryFleetNodeMiner re-resolves per command so
	// an unpair/revoke takes effect immediately (the remote miner holds no live connection,
	// so re-resolving is cheap).
	if m, ok, err := s.tryFleetNodeMiner(ctx, deviceID, mode); err != nil {
		return nil, err
	} else if ok {
		return m, nil
	}

	deviceData, err := s.GetQueries(ctx).GetDeviceWithCredentialsAndIPByDeviceIdentifier(ctx, sqlc.GetDeviceWithCredentialsAndIPByDeviceIdentifierParams{
		DeviceIdentifier:       string(deviceID),
		IncludeDefaultPassword: mode == minerResolutionPairedLike,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("device not found: %s", deviceID)
		}
		return nil, fmt.Errorf("failed to get device data: %w", err)
	}

	deviceModel := ""
	if deviceData.Model.Valid {
		deviceModel = deviceData.Model.String
	}
	deviceManufacturer := ""
	if deviceData.Manufacturer.Valid {
		deviceManufacturer = deviceData.Manufacturer.String
	}

	var siteID int64
	if deviceData.SiteID.Valid {
		siteID = deviceData.SiteID.Int64
	}

	m, err := s.createMiner(
		ctx,
		deviceData.DeviceIdentifier,
		deviceData.OrgID,
		siteID,
		deviceData.Port,
		deviceData.DriverName,
		deviceManufacturer,
		deviceModel,
		deviceData.UsernameEnc.String,
		deviceData.PasswordEnc.String,
		deviceData.IpAddress,
		deviceData.UrlScheme,
		deviceData.SerialNumber.String,
		deviceData.MacAddress,
	)
	if err != nil {
		return nil, err
	}

	s.cache.Add(cacheKey, m)
	return m, nil
}

// tryFleetNodeMiner returns a remote-node Miner if the device is paired to an active
// fleet node. ok=false (nil error) means not fleet-node paired (or routing disabled),
// so the caller dials directly.
func (s *Service) tryFleetNodeMiner(ctx context.Context, deviceID models.DeviceIdentifier, mode minerResolutionMode) (interfaces.Miner, bool, error) {
	if s.commandSender == nil {
		return nil, false, nil
	}
	row, err := s.GetQueries(ctx).GetActiveFleetNodeForDevice(ctx, sqlc.GetActiveFleetNodeForDeviceParams{
		DeviceIdentifier:       string(deviceID),
		IncludeDefaultPassword: mode == minerResolutionPairedLike,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to resolve fleet node for device: %w", err)
	}
	// No server-side plugin gate: the fleet node (not the server) dials the miner
	// and loads the driver plugin; the server only routes the command.
	m, err := remotenode.New(remotenode.Config{
		Sender:           s.commandSender,
		Gate:             s.nodeLimiter,
		FleetNodeID:      row.FleetNodeID,
		OrgID:            row.OrgID,
		SiteID:           row.SiteID.Int64,
		DeviceIdentifier: row.DeviceIdentifier,
		DriverName:       row.DriverName,
		IPAddress:        row.IpAddress,
		Port:             row.Port,
		URLScheme:        row.UrlScheme,
		SerialNumber:     row.SerialNumber.String,
		MacAddress:       row.MacAddress,
		// Credential intentionally empty: the node's per-org decryption key is a pairing
		// concern. No-secret drivers (e.g. virtual) work end to end now.
	})
	if err != nil {
		return nil, false, err
	}
	return m, true, nil
}

// InvalidateMiner removes the cached miner handle for the given device identifier
// so the next lookup fetches fresh credentials and connection info from the DB.
// Call this on auth errors, credential changes, and device lifecycle events
// (unpair, delete).
func (s *Service) InvalidateMiner(deviceIdentifier models.DeviceIdentifier) {
	s.cache.Remove(minerCacheKey(minerResolutionPairedOnly, deviceIdentifier))
	s.cache.Remove(minerCacheKey(minerResolutionPairedLike, deviceIdentifier))
}

// InvalidateMinerByID evicts the cached handle for a device id (resolving its identifier
// first) so a pair/unpair transition doesn't leave a stale direct handle dialing past the
// fleet-node route. Best-effort: a lookup miss is a no-op.
func (s *Service) InvalidateMinerByID(ctx context.Context, deviceID int64) {
	// Runs after a pair/unpair commit, so detach from the caller ctx (a client
	// disconnect must not skip the eviction) and bound the lookup.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	identifier, err := s.GetQueries(ctx).GetDeviceIdentifierByID(ctx, deviceID)
	if err != nil {
		slog.Warn("miner cache invalidation: device id lookup failed; stale handle may persist until cache TTL",
			"device_id", deviceID, "err", err)
		return
	}
	s.InvalidateMiner(models.DeviceIdentifier(identifier))
}

func minerCacheKey(mode minerResolutionMode, deviceID models.DeviceIdentifier) string {
	return string(mode) + ":" + string(deviceID)
}

func (s *Service) getProtoMinerAuthPrivateKey(ctx context.Context, orgID int64) ([]byte, error) {
	encryptedKey, err := s.userStore.GetOrganizationPrivateKey(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting org private key: %v", err)
	}

	privateKey, err := s.encryptService.Decrypt(encryptedKey)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error decrypting private key: %v", err)
	}

	return privateKey, nil
}

func (s *Service) createMiner(ctx context.Context, deviceIdentifier string, orgID int64, siteID int64, devicePort string, driverName string, deviceManufacturer string, deviceModel string, deviceUsername string, devicePassword string, deviceIPAddress string, deviceScheme string, deviceSerialNumber string, macAddress string) (interfaces.Miner, error) {
	if !s.pluginManager.HasPluginForDriverName(driverName) {
		return nil, fmt.Errorf("no plugin available (driver_name=%q) — ensure the device has been discovered and the appropriate plugin is loaded", driverName)
	}
	return plugins.NewPluginMinerWithCredentials(ctx, plugins.PluginMinerConfig{
		DeviceIdentifier:   deviceIdentifier,
		DriverName:         driverName,
		Caps:               s.effectiveCapabilitiesForDevice(ctx, driverName, deviceManufacturer, deviceModel),
		DeviceIPAddress:    deviceIPAddress,
		DevicePort:         devicePort,
		DeviceScheme:       deviceScheme,
		DeviceSerialNumber: deviceSerialNumber,
		DeviceUsername:     deviceUsername,
		DevicePassword:     devicePassword,
		MacAddress:         macAddress,
		OrgID:              orgID,
		SiteID:             siteID,
		EncryptService:     s.encryptService,
		TokenService:       s.tokenService,
		FilesService:       s.filesService,
		GetOrgPrivateKey:   s.getProtoMinerAuthPrivateKey,
		DriverGetter:       s.pluginManager,
	})
}

func (s *Service) effectiveCapabilitiesForDevice(ctx context.Context, driverName string, deviceManufacturer string, deviceModel string) sdk.Capabilities {
	caps := sdk.Capabilities{}
	for capability, enabled := range s.pluginManager.GetCapabilitiesForDriverName(driverName) {
		caps[capability] = enabled
	}

	if deviceModel == "" {
		return caps
	}

	driver, err := s.pluginManager.GetDriverByDriverName(driverName)
	if err != nil {
		return caps
	}

	modelProvider, ok := driver.(sdk.ModelCapabilitiesProvider)
	if !ok {
		return caps
	}

	modelCaps := modelProvider.GetCapabilitiesForModel(ctx, deviceManufacturer, deviceModel)
	if modelCaps == nil {
		return caps
	}

	for capability, enabled := range modelCaps {
		caps[capability] = enabled
	}
	return caps
}
