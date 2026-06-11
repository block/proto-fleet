package pairing

import (
	"context"
	"log/slog"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
)

// maxPairBatch caps targets per pair command, matching FleetNodePairRequest.targets
// max_items so a pair_all on a huge fleet can't balloon one ControlCommand; the
// operator re-issues for the remainder (paired devices drop from the listing).
const maxPairBatch = 1024

// ResolvePairTargets returns the pairable targets for a batch request. It draws
// from the not-yet-paired devices the node discovered (the listing already
// excludes cloud-paired and already-bound devices), so a requested identifier
// that is not pairable is silently dropped. Explicit selections are filtered in
// SQL by identifier (no whole-org scan); pair_all is capped at one batch.
func (s *Service) ResolvePairTargets(ctx context.Context, fleetNodeID, orgID int64, identifiers []string, pairAllUnpaired bool, credentials *pairingpb.Credentials) ([]*pairingpb.FleetNodePairTarget, error) {
	var (
		ids   []string
		limit *int64
	)
	if pairAllUnpaired {
		l := int64(maxPairBatch)
		limit = &l
	} else {
		// Non-nil (even empty) so the SQL filter means "only these", not "all".
		ids = identifiers
		if ids == nil {
			ids = []string{}
		}
	}
	// A pair-all without usable basic-auth credentials can't satisfy
	// AUTHENTICATION_NEEDED rows, so excluding them stops unsatisfiable rows from
	// starving never-attempted devices on re-issue. Usable = password set (matches
	// the node's secretBundleFor); explicit selection still targets them.
	usableCredentials := credentials != nil && credentials.Password != nil
	excludeAuthNeeded := pairAllUnpaired && !usableCredentials
	candidates, err := s.store.ListFleetNodeDiscoveredDevices(ctx, orgID, &fleetNodeID, ids, nil, limit, excludeAuthNeeded)
	if err != nil {
		return nil, fleeterror.LogInternal(component, "list pair candidates", clientErrList, err)
	}
	targets := make([]*pairingpb.FleetNodePairTarget, 0, len(candidates))
	for _, c := range candidates {
		targets = append(targets, &pairingpb.FleetNodePairTarget{
			DeviceIdentifier: c.DeviceIdentifier,
			IpAddress:        c.IPAddress,
			Port:             c.Port,
			UrlScheme:        c.URLScheme,
			DriverName:       c.DriverName,
			Manufacturer:     c.Manufacturer,
			FirmwareVersion:  c.FirmwareVersion,
		})
	}
	return targets, nil
}

// PersistFleetNodePairResult records one device's reported pairing outcome in a
// single transaction and returns the resulting status. PAIRED binds the device to
// the node and stores basic-auth credentials; AUTH_NEEDED/AUTH_FAILED record
// AUTHENTICATION_NEEDED for retry; ERROR/UNSPECIFIED persist nothing.
func (s *Service) PersistFleetNodePairResult(ctx context.Context, fleetNodeID, orgID int64, result *gatewaypb.FleetNodePairResult, assignedBy *int64) (string, error) {
	if s.deviceStore == nil || s.discoveredDeviceStore == nil || s.encryptService == nil {
		return "", fleeterror.NewInternalError("fleet node pairing provisioning is not configured")
	}
	identifier := result.GetDeviceIdentifier()
	outcome := result.GetOutcome()
	if outcome == gatewaypb.PairOutcome_PAIR_OUTCOME_ERROR || outcome == gatewaypb.PairOutcome_PAIR_OUTCOME_UNSPECIFIED {
		return StatusFailed, nil
	}

	doi := discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: identifier, OrgID: orgID}
	// Default to the outcome's status; the downgrade guard below may override it.
	persisted := StatusAuthenticationNeeded
	if outcome == gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED {
		persisted = StatusPaired
	}
	conflict := false
	txErr := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		dd, err := s.discoveredDeviceStore.GetDevice(ctx, doi)
		if err != nil {
			if fleeterror.IsNotFoundError(err) {
				return fleeterror.NewNotFoundError("discovered device not found")
			}
			return fleeterror.LogInternal(component, "load discovered device", clientErrPair, err)
		}
		// Only the owning node may report results for this device.
		if dd.DiscoveredByFleetNodeID == nil || *dd.DiscoveredByFleetNodeID != fleetNodeID {
			return fleeterror.NewFailedPreconditionError("device was not discovered by this fleet node")
		}

		existing, err := s.deviceStore.GetDeviceByDeviceIdentifier(ctx, identifier, orgID)
		if err != nil && !fleeterror.IsNotFoundError(err) {
			return fleeterror.LogInternal(component, "lookup device", clientErrLookupDeviceForPairing, err)
		}

		// A non-PAIRED report must not downgrade an already-PAIRED device: between
		// target resolution and now, the cloud or another node may have paired it.
		// Check before any write and return its real status untouched. A freshly
		// inserted device (existing == nil) can't be paired yet.
		if outcome != gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED && existing != nil {
			deviceID, err := s.store.GetDeviceIDByDeviceIdentifier(ctx, identifier)
			if err != nil {
				return fleeterror.LogInternal(component, "resolve device id", clientErrPair, err)
			}
			paired, err := s.store.DeviceHasActivePairing(ctx, deviceID, orgID)
			if err != nil {
				return fleeterror.LogInternal(component, "check active pairing", clientErrPair, err)
			}
			if paired {
				persisted = StatusPaired
				return nil
			}
		}

		applyReportedIdentity(dd, result)
		if _, err := s.discoveredDeviceStore.Save(ctx, doi, dd); err != nil {
			return fleeterror.LogInternal(component, "save discovered device", clientErrPair, err)
		}
		if existing == nil {
			if err := s.deviceStore.InsertDevice(ctx, &dd.Device, orgID, identifier); err != nil {
				if db.IsUniqueViolationError(err) {
					conflict = true
					return err
				}
				return fleeterror.LogInternal(component, "insert device", clientErrPair, err)
			}
		} else {
			if err := s.deviceStore.UpdateDeviceInfo(ctx, &dd.Device, orgID); err != nil {
				if db.IsUniqueViolationError(err) {
					conflict = true
					return err
				}
				return fleeterror.LogInternal(component, "update device", clientErrPair, err)
			}
		}

		if outcome != gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED {
			// Closes the TOCTOU left by the guard read above: if a concurrent pair
			// turned this device PAIRED meanwhile, the write no-ops (applied == false)
			// and we report the real PAIRED status instead of downgrading it.
			applied, err := s.deviceStore.SetDevicePairingAuthNeededIfNotPaired(ctx, &dd.Device, orgID)
			if err != nil {
				return fleeterror.LogInternal(component, "set auth-needed", clientErrPair, err)
			}
			if !applied {
				persisted = StatusPaired
			}
			return nil
		}

		// PAIRED: bind to the node BEFORE marking PAIRED, so the cloud-paired guard
		// (PAIRED AND NOT node-bound) never sees a PAIRED-but-unbound row.
		deviceID, err := s.store.GetDeviceIDByDeviceIdentifier(ctx, identifier)
		if err != nil {
			return fleeterror.LogInternal(component, "resolve device id", clientErrPair, err)
		}
		if err := s.pairDeviceLocked(ctx, fleetNodeID, deviceID, orgID, assignedBy); err != nil {
			return err
		}
		if creds := nodeUsedCredentials(result); creds != nil {
			if err := s.saveMinerCredentials(ctx, &dd.Device, orgID, creds); err != nil {
				return err
			}
		}
		if err := s.deviceStore.UpsertDevicePairing(ctx, &dd.Device, orgID, StatusPaired); err != nil {
			return fleeterror.LogInternal(component, "set paired", clientErrPair, err)
		}
		// Reachable during pairing, so seed an ACTIVE status.
		if err := s.deviceStore.UpsertDeviceStatus(ctx, minermodels.DeviceIdentifier(identifier), minermodels.MinerStatusActive, ""); err != nil {
			return fleeterror.LogInternal(component, "set device status", clientErrPair, err)
		}
		return nil
	})
	if conflict {
		// The reported serial/identifier already belongs to another non-deleted
		// device (e.g. an auto:* probe that couldn't read the serial pre-auth now
		// reports one already registered). Surface a clean FAILED so the operator can
		// reconcile, rather than an opaque Internal error; retrying won't clear it.
		slog.Warn("fleet node pair result conflicts with an existing device; not persisted",
			"fleet_node_id", fleetNodeID, "device_identifier", identifier, "serial_number", result.GetSerialNumber())
		return StatusFailed, nil
	}
	if txErr != nil {
		return "", txErr
	}
	return persisted, nil
}

// nodeUsedCredentials returns the credentials to persist for a successful pairing.
// The cloud can't verify a driver's auth mechanism in server mode, so the node's
// used_credentials presence is the password-auth signal: present (even blank) ->
// store as reported; nil -> asymmetric auth, store nothing.
func nodeUsedCredentials(result *gatewaypb.FleetNodePairResult) *pairingpb.Credentials {
	uc := result.GetUsedCredentials()
	if uc == nil {
		return nil
	}
	pw := uc.GetPassword()
	return &pairingpb.Credentials{Username: uc.GetUsername(), Password: &pw}
}

func (s *Service) saveMinerCredentials(ctx context.Context, device *pairingpb.Device, orgID int64, creds *pairingpb.Credentials) error {
	encUser, err := s.encryptService.Encrypt([]byte(creds.GetUsername()))
	if err != nil {
		return fleeterror.NewInternalErrorf("encrypt username: %v", err)
	}
	encPass, err := s.encryptService.Encrypt([]byte(creds.GetPassword()))
	if err != nil {
		return fleeterror.NewInternalErrorf("encrypt password: %v", err)
	}
	if err := s.deviceStore.UpsertMinerCredentials(ctx, device, orgID, encUser, secrets.NewText(encPass)); err != nil {
		return fleeterror.LogInternal(component, "save credentials", clientErrPair, err)
	}
	return nil
}

// applyReportedIdentity folds the identity the node learned during pairing into
// the discovered device so the device row and discovery row reflect post-pair truth.
func applyReportedIdentity(dd *discoverymodels.DiscoveredDevice, result *gatewaypb.FleetNodePairResult) {
	if v := result.GetSerialNumber(); v != "" {
		dd.SerialNumber = v
	}
	if v := result.GetMacAddress(); v != "" {
		dd.MacAddress = networking.NormalizeMAC(v)
	}
	if v := result.GetModel(); v != "" {
		dd.Model = v
	}
	if v := result.GetManufacturer(); v != "" {
		dd.Manufacturer = v
	}
	if v := result.GetFirmwareVersion(); v != "" {
		dd.FirmwareVersion = v
	}
}
