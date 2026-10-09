package miner

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/credentialblob"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	lru "github.com/hashicorp/golang-lru/v2/expirable"
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

var _ telemetry.CachedMinerGetter = &Service{}

type Service struct {
	// TODO: Refactor this to use a store instead of SQLConnectionManager directly
	sqlstores.SQLConnectionManager
	userStore      stores.UserStore
	encryptService *encrypt.Service
	filesService   *files.Service
	writeService   *devicewrite.Service
	pluginManager  PluginManager

	// commandSender, when set, routes commands for fleet-node-paired devices over the
	// ControlStream; nil disables routing (every device resolves to a direct PluginMiner).
	commandSender remotenode.CommandSender
	// nodeLimiter paces commands per fleet node so a large batch can't oversubscribe
	// a node. Shared across all remote-node miners (keyed by fleet_node id).
	nodeLimiter               remotenode.Gate
	nodeDeferrableReadLimiter remotenode.Gate
	nodeLogDownloadLimiter    remotenode.Gate

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
	deferrableLimiter := remotenode.NewPerNodeLimiter(remotenode.DefaultPerNodeDeferrableReadLimit)
	s.nodeDeferrableReadLimiter = remotenode.NewNestedGate(deferrableLimiter, s.nodeLimiter)
	s.nodeLogDownloadLimiter = remotenode.NewPerNodeLimiter(remotenode.DefaultPerNodeLogDownloadLimit)
	return s
}

// PluginManager defines the interface for plugin manager operations needed by MinerService
type PluginManager interface {
	HasPluginForDriverName(driverName string) bool
	GetCapabilitiesForDriverName(driverName string) sdk.Capabilities
	plugins.PluginDriverGetter
}

func NewMinerService(db *sql.DB, userStore stores.UserStore, encryptService *encrypt.Service, filesService *files.Service, pluginManager PluginManager) *Service {
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
		writeService:         devicewrite.NewService(db),
		pluginManager:        pluginManager,
		cache:                lru.NewLRU[string, interfaces.Miner](minerCacheSize, nil, minerCacheTTL),
	}
}

// GetMiner returns the miner handle for the given numeric device ID.
// It performs a lightweight identifier lookup then delegates to
// GetMinerFromDeviceIdentifier so both lookup paths share the same cache.
func (s *Service) GetMiner(ctx context.Context, deviceID int64) (interfaces.Miner, error) {
	identifier, err := s.GetQueries(ctx).GetDeviceIdentifierByID(ctx, deviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("device not found: %d", deviceID)
		}
		return nil, fmt.Errorf("failed to get device identifier: %w", err)
	}
	return s.GetMinerFromDeviceIdentifier(ctx, models.DeviceIdentifier(identifier))
}

func (s *Service) GetMinerFromDeviceIdentifier(ctx context.Context, deviceID models.DeviceIdentifier) (interfaces.Miner, error) {
	return s.getMinerFromDeviceIdentifier(ctx, deviceID, nil, true)
}

// GetMinerForCommand resolves the current binding once for a new execution.
// Cached read handles can predate a site, route, credential or epoch change;
// they remain independently guarded and must not be refreshed during dispatch.
func (s *Service) GetMinerForCommand(ctx context.Context, deviceID int64) (interfaces.Miner, error) {
	identifier, err := s.GetQueries(ctx).GetDeviceIdentifierByID(ctx, deviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("device not found: %d", deviceID)
		}
		return nil, fmt.Errorf("failed to get device identifier: %w", err)
	}
	return s.getMinerFromDeviceIdentifier(ctx, models.DeviceIdentifier(identifier), nil, false)
}

func (s *Service) GetMinerForPasswordUpdate(ctx context.Context, deviceID int64, currentPassword string) (interfaces.Miner, error) {
	identifier, err := s.GetQueries(ctx).GetDeviceIdentifierByID(ctx, deviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("device not found: %d", deviceID)
		}
		return nil, fmt.Errorf("failed to get device identifier: %w", err)
	}

	return s.getMinerFromDeviceIdentifier(ctx, models.DeviceIdentifier(identifier), &sdk.UsernamePassword{
		Username: models.ProtoDefaultUsername,
		Password: currentPassword,
	}, false)
}

func (s *Service) getMinerFromDeviceIdentifier(ctx context.Context, deviceID models.DeviceIdentifier, protoMissingCredentials *sdk.UsernamePassword, useCache bool) (interfaces.Miner, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("device ID cannot be empty")
	}

	if useCache {
		if m, ok := s.cache.Get(string(deviceID)); ok {
			return m, nil
		}
	}

	m, err := s.resolveMiner(ctx, deviceID, protoMissingCredentials)
	if err != nil {
		return nil, err
	}
	if useCache {
		s.cache.Add(string(deviceID), m)
	}
	return m, nil
}

func (s *Service) resolveMiner(ctx context.Context, deviceID models.DeviceIdentifier, protoMissingCredentials *sdk.UsernamePassword) (interfaces.Miner, error) {
	if m, ok, err := s.tryFleetNodeMiner(ctx, deviceID); err != nil {
		return nil, err
	} else if ok {
		return m, nil
	}

	deviceData, err := s.GetQueries(ctx).GetDeviceWithCredentialsAndIPByDeviceIdentifier(ctx, string(deviceID))
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

	deviceUsername := deviceData.UsernameEnc.String
	devicePassword := deviceData.PasswordEnc.String
	useTransientCredentials := protoMissingCredentials != nil &&
		deviceData.DriverName == models.DriverNameProto &&
		!deviceData.UsernameEnc.Valid && !deviceData.PasswordEnc.Valid
	if useTransientCredentials {
		deviceUsername, devicePassword, err = s.encryptTransientCredentials(*protoMissingCredentials)
		if err != nil {
			return nil, err
		}
	}

	if !s.pluginManager.HasPluginForDriverName(deviceData.DriverName) {
		return nil, fmt.Errorf("no plugin available (driver_name=%q) — ensure the device has been discovered and the appropriate plugin is loaded", deviceData.DriverName)
	}
	// SDK scope excludes facade-only metadata; write gates still capture the entire fresh row.
	sdkBinding := deviceData
	sdkBinding.SiteID = sql.NullInt64{}
	sdkBinding.Model = sql.NullString{}
	sdkBinding.Manufacturer = sql.NullString{}
	scope, err := devicewrite.DirectBindingHash(sdkBinding)
	if err != nil {
		return nil, err
	}
	// Password recovery gets a fresh scoped backend, preserving generation fencing
	// without reusing transient credentials or bypassing normal scoped resolution.
	if useTransientCredentials {
		scope += ":password-recovery:" + id.GenerateID()
	}
	m, err := plugins.NewPluginMinerWithCredentials(ctx, plugins.PluginMinerConfig{
		SDKReuseScopeKey:   scope,
		DeviceClass:        sdk.DeviceClass(deviceData.DeviceClass),
		RPCPort:            deviceData.RpcPort,
		RPCTLS:             deviceData.RpcTls,
		DeviceIdentifier:   deviceData.DeviceIdentifier,
		DriverName:         deviceData.DriverName,
		Caps:               s.effectiveCapabilitiesForDevice(ctx, deviceData.DriverName, deviceManufacturer, deviceModel),
		DeviceIPAddress:    deviceData.IpAddress,
		DevicePort:         deviceData.Port,
		DeviceScheme:       deviceData.UrlScheme,
		DeviceSerialNumber: deviceData.SerialNumber.String,
		DeviceUsername:     deviceUsername,
		DevicePassword:     devicePassword,
		MacAddress:         deviceData.MacAddress,
		OrgID:              deviceData.OrgID,
		SiteID:             siteID,
		EncryptService:     s.encryptService,
		FilesService:       s.filesService,
		DriverGetter:       s.pluginManager,
		WriteGate:          s.directWriteGate(deviceData),
	})
	if err != nil {
		return nil, err
	}

	hash, err := devicewrite.DirectBindingHash(deviceData)
	if err != nil {
		return nil, err
	}
	direct, ok := m.(*plugins.PluginMiner)
	if !ok {
		return nil, fleeterror.NewInternalError("direct miner adapter could not be bound")
	}
	return &directMinerHandle{PluginMiner: direct, owner: s, deviceID: deviceData.ID, organizationID: deviceData.OrgID, bindingHash: hash}, nil
}

func (s *Service) encryptTransientCredentials(credentials sdk.UsernamePassword) (string, string, error) {
	usernameEnc, err := s.encryptService.Encrypt([]byte(credentials.Username))
	if err != nil {
		return "", "", fleeterror.NewInternalErrorf("failed to encrypt username: %v", err)
	}
	passwordEnc, err := s.encryptService.Encrypt([]byte(credentials.Password))
	if err != nil {
		return "", "", fleeterror.NewInternalErrorf("failed to encrypt password: %v", err)
	}
	return usernameEnc, passwordEnc, nil
}

// tryFleetNodeMiner returns a remote-node Miner if the device is paired to an active
// fleet node. ok=false (nil error) means not fleet-node paired (or routing disabled),
// so the caller dials directly.
func (s *Service) tryFleetNodeMiner(ctx context.Context, deviceID models.DeviceIdentifier) (interfaces.Miner, bool, error) {
	if s.commandSender == nil {
		return nil, false, nil
	}
	telemetryRoute, err := s.GetQueries(ctx).GetFleetNodeTelemetryRouteByDeviceIdentifier(ctx, string(deviceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to resolve fleet node telemetry route: %w", err)
	}
	// No server-side plugin gate: the fleet node (not the server) dials the miner
	// and loads the driver plugin; the server only routes the command.
	remoteCommandMiner, err := remotenode.New(remotenode.Config{
		DeviceClass: commonpb.DeviceClass(telemetryRoute.DeviceClass),
		Sender:      s.commandSender,
		EncryptionPublicKey: func(ctx context.Context) ([]byte, error) {
			return s.poolConfigEncryptionPublicKey(ctx, telemetryRoute.OrgID, telemetryRoute.DeviceIdentifier, telemetryRoute.FleetNodeID)
		},
		WriteGate:          s.remoteWriteGate(telemetryRoute),
		Gate:               s.nodeLimiter,
		DeferrableReadGate: s.nodeDeferrableReadLimiter,
		LogDownloadGate:    s.nodeLogDownloadLimiter,
		LogArtifacts:       s.filesService,
		FleetNodeID:        telemetryRoute.FleetNodeID,
		OrgID:              telemetryRoute.OrgID,
		SiteID:             telemetryRoute.SiteID.Int64,
		DeviceIdentifier:   telemetryRoute.DeviceIdentifier,
		DriverName:         telemetryRoute.DriverName,
		IPAddress:          telemetryRoute.IpAddress,
		Port:               telemetryRoute.Port,
		URLScheme:          telemetryRoute.UrlScheme,
		RPCPort:            telemetryRoute.RpcPort,
		RPCTLS:             telemetryRoute.RpcTls,
		SerialNumber:       telemetryRoute.SerialNumber.String,
		MacAddress:         telemetryRoute.MacAddress,
		CredentialUsername: fleetNodeCredentialBytes(telemetryRoute.UsernameEnc),
		CredentialPassword: fleetNodeCredentialBytes(telemetryRoute.PasswordEnc),
	})
	if err != nil {
		return nil, false, err
	}
	remoteRoute, err := s.remoteRouteFromRow(telemetryRoute)
	if err != nil {
		return nil, false, err
	}
	telemetryMiner, err := newRemoteFleetNodeMiner(remoteRoute, s.commandSender, s.nodeDeferrableReadLimiter, remoteCommandMiner)
	if err != nil {
		return nil, false, err
	}
	return telemetryMiner, true, nil
}

// Resolve the key at write time so cached miner handles cannot use a rotated key
// or send credentials to a node after the device has moved.
func (s *Service) poolConfigEncryptionPublicKey(ctx context.Context, orgID int64, deviceIdentifier string, fleetNodeID int64) ([]byte, error) {
	routes, err := s.GetQueries(ctx).GetDeviceCommandRoutes(ctx, sqlc.GetDeviceCommandRoutesParams{
		OrgID:             orgID,
		DeviceIdentifiers: []string{deviceIdentifier},
	})
	if err != nil {
		return nil, fmt.Errorf("resolve pool configuration encryption key: %w", err)
	}
	if len(routes) != 1 || routes[0].DeviceIdentifier != deviceIdentifier || !routes[0].FleetNodeID.Valid || routes[0].FleetNodeID.Int64 != fleetNodeID {
		return nil, errors.New("pool configuration node route changed")
	}
	return append([]byte(nil), routes[0].EncryptionPubkey...), nil
}

func fleetNodeCredentialBytes(value sql.NullString) []byte {
	if !value.Valid {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value.String)
	if err != nil || !credentialblob.IsValid(decoded) {
		return nil
	}
	return decoded
}

// InvalidateMiner removes the cached miner handle for the given device identifier
// so the next lookup fetches fresh credentials and connection info from the DB.
// Call this on auth errors, credential changes, and device lifecycle events
// (unpair, delete).
func (s *Service) InvalidateMiner(deviceIdentifier models.DeviceIdentifier) {
	s.cache.Remove(string(deviceIdentifier))
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
