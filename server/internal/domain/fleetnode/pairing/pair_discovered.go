package pairing

import (
	"context"
	"encoding/base64"
	"log/slog"

	fleetmanagementv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	minercommandv1 "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
)

// MaxPairBatch caps targets per pair command, matching FleetNodePairRequest.targets
// max_items so a pair_all on a huge fleet can't balloon one ControlCommand; the
// operator re-issues for the remainder (paired devices drop from the listing).
const MaxPairBatch = 1024

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
		l := int64(MaxPairBatch)
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
	candidates, err := s.store.ListFleetNodeDiscoveredDevices(ctx, orgID, &fleetNodeID, FleetNodeDiscoveredDeviceFilter{
		Identifiers:       ids,
		Limit:             limit,
		ExcludeAuthNeeded: excludeAuthNeeded,
	})
	if err != nil {
		return nil, fleeterror.LogInternal(component, "list pair candidates", clientErrList, err)
	}
	return pairTargetsFromDiscoveredDevices(candidates), nil
}

// ResolvePairTargetsByFilterPage returns pairable node-discovered targets that
// match the DeviceFilter shape PairingService.Pair accepts for allDevices
// requests. nextCursor is non-nil when another page may exist. Fleet-node-
// discovered rows do not have every fleet-list attribute, so this intentionally
// supports only filters represented in the discovered-device table/listing.
// Unsupported/unsatisfiable filters return no targets so the caller can safely
// leave those requests to the server-local pairing path.
func (s *Service) ResolvePairTargetsByFilterPage(ctx context.Context, fleetNodeID, orgID int64, filter *minercommandv1.DeviceFilter, credentials *pairingpb.Credentials, cursorID *int64) ([]*pairingpb.FleetNodePairTarget, *int64, error) {
	if filter == nil || unsupportedFleetNodePairFilter(filter) {
		return nil, nil, nil
	}
	statuses, supported := pairingStatusFilterValues(filter.GetPairingStatus())
	if !supported {
		return nil, nil, nil
	}

	usableCredentials := credentials != nil && credentials.Password != nil
	excludeAuthNeeded := !usableCredentials
	limit := int64(MaxPairBatch)
	candidates, err := s.store.ListFleetNodeDiscoveredDevices(ctx, orgID, &fleetNodeID, FleetNodeDiscoveredDeviceFilter{
		PairingStatuses:   statuses,
		Models:            nonEmptyStrings(filter.GetModels()),
		Manufacturers:     nonEmptyStrings(filter.GetManufacturers()),
		CursorID:          cursorID,
		Limit:             &limit,
		ExcludeAuthNeeded: excludeAuthNeeded,
	})
	if err != nil {
		return nil, nil, fleeterror.LogInternal(component, "list pair candidates", clientErrList, err)
	}
	var nextCursor *int64
	if len(candidates) == MaxPairBatch {
		last := candidates[len(candidates)-1].ID
		nextCursor = &last
	}
	return pairTargetsFromDiscoveredDevices(candidates), nextCursor, nil
}

func nonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}

func unsupportedFleetNodePairFilter(filter *minercommandv1.DeviceFilter) bool {
	return len(filter.GetDeviceStatus()) > 0
}

func pairTargetsFromDiscoveredDevices(devices []FleetNodeDiscoveredDevice) []*pairingpb.FleetNodePairTarget {
	targets := make([]*pairingpb.FleetNodePairTarget, 0, len(devices))
	for _, d := range devices {
		targets = append(targets, &pairingpb.FleetNodePairTarget{
			DeviceIdentifier: d.DeviceIdentifier,
			IpAddress:        d.IPAddress,
			Port:             d.Port,
			UrlScheme:        d.URLScheme,
			DriverName:       d.DriverName,
			Manufacturer:     d.Manufacturer,
			FirmwareVersion:  d.FirmwareVersion,
		})
	}
	return targets
}

func pairingStatusFilterValues(statuses []fleetmanagementv1.PairingStatus) ([]string, bool) {
	if len(statuses) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(statuses))
	for _, status := range statuses {
		switch status { //nolint:exhaustive // Unsupported pairing statuses intentionally fail closed.
		case fleetmanagementv1.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED:
			out = append(out, StatusAuthenticationNeeded)
		case fleetmanagementv1.PairingStatus_PAIRING_STATUS_UNPAIRED:
			out = append(out, "", StatusUnpaired)
		case fleetmanagementv1.PairingStatus_PAIRING_STATUS_FAILED:
			out = append(out, StatusFailed)
		case fleetmanagementv1.PairingStatus_PAIRING_STATUS_UNSPECIFIED,
			fleetmanagementv1.PairingStatus_PAIRING_STATUS_PAIRED,
			fleetmanagementv1.PairingStatus_PAIRING_STATUS_PENDING,
			fleetmanagementv1.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD:
			continue
		default:
			return nil, false
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// PersistFleetNodePairResult records one device's reported pairing outcome in a
// single transaction and returns the resulting status. PAIRED binds the device to
// the node and stores node-encrypted credentials when reported; AUTH_NEEDED/AUTH_FAILED
// record AUTHENTICATION_NEEDED for retry; ERROR/UNSPECIFIED persist nothing.
func (s *Service) PersistFleetNodePairResult(ctx context.Context, fleetNodeID, orgID int64, result *gatewaypb.FleetNodePairResult, assignedBy *int64) (string, error) {
	if s.deviceStore == nil || s.discoveredDeviceStore == nil {
		return "", fleeterror.NewInternalError("fleet node pairing provisioning is not configured")
	}
	identifier := result.GetDeviceIdentifier()
	outcome := result.GetOutcome()
	if outcome == gatewaypb.PairOutcome_PAIR_OUTCOME_ERROR || outcome == gatewaypb.PairOutcome_PAIR_OUTCOME_UNSPECIFIED {
		return StatusFailed, nil
	}

	doi := discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: identifier, OrgID: orgID}
	// Default to the outcome's status; the downgrade guard below may override it.
	defaultPersisted := StatusAuthenticationNeeded
	if outcome == gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED {
		defaultPersisted = StatusPaired
		if result.DefaultPasswordActive != nil && result.GetDefaultPasswordActive() {
			defaultPersisted = StatusDefaultPassword
		}
	}
	persisted := defaultPersisted
	conflict := false
	var boundDeviceID int64
	txErr := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		persisted = defaultPersisted
		conflict = false
		boundDeviceID = 0

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

		// default_password_active is tri-state: absent means the node/plugin could
		// not determine whether factory credentials are still active. Preserve an
		// existing DEFAULT_PASSWORD remediation state unless the report explicitly
		// says false.
		if outcome == gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED && result.DefaultPasswordActive == nil && existing != nil {
			status, err := s.deviceStore.GetDevicePairingStatusByIdentifier(ctx, identifier, orgID)
			if err != nil {
				if !fleeterror.IsNotFoundError(err) {
					return fleeterror.LogInternal(component, "load existing pairing status", clientErrPair, err)
				}
			} else if status == StatusDefaultPassword {
				persisted = StatusDefaultPassword
			}
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
				status, err := s.deviceStore.GetDevicePairingStatusByIdentifier(ctx, identifier, orgID)
				if err != nil {
					return fleeterror.LogInternal(component, "load active pairing status", clientErrPair, err)
				}
				persisted = status
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
			// turned this device paired-like meanwhile, the write no-ops (applied == false)
			// and we report the real paired-like status instead of downgrading it.
			applied, err := s.deviceStore.SetDevicePairingAuthNeededIfNotPaired(ctx, &dd.Device, orgID)
			if err != nil {
				return fleeterror.LogInternal(component, "set auth-needed", clientErrPair, err)
			}
			if !applied {
				status, err := s.deviceStore.GetDevicePairingStatusByIdentifier(ctx, identifier, orgID)
				if err != nil {
					return fleeterror.LogInternal(component, "load active pairing status", clientErrPair, err)
				}
				persisted = status
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
		boundDeviceID = deviceID
		if encrypted := result.GetEncryptedCredentials(); encrypted != nil {
			if err := s.saveFleetNodeEncryptedCredentials(ctx, &dd.Device, orgID, encrypted); err != nil {
				return err
			}
		}
		if err := s.deviceStore.UpsertDevicePairing(ctx, &dd.Device, orgID, persisted); err != nil {
			return fleeterror.LogInternal(component, "set pairing status", clientErrPair, err)
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
	// Evict any stale direct handle so the next command re-resolves over the ControlStream
	// (mirrors PairDevice). boundDeviceID is set only on the true PAIRED bind path.
	if boundDeviceID != 0 && s.invalidateMiner != nil {
		s.invalidateMiner(ctx, boundDeviceID)
	}
	return persisted, nil
}

func (s *Service) saveFleetNodeEncryptedCredentials(ctx context.Context, device *pairingpb.Device, orgID int64, encrypted *gatewaypb.EncryptedCredentials) error {
	if len(encrypted.GetUsername()) == 0 || len(encrypted.GetPassword()) == 0 {
		return fleeterror.NewFailedPreconditionError("encrypted credentials must include username and password")
	}
	encodedUsername := base64.StdEncoding.EncodeToString(encrypted.GetUsername())
	encodedPassword := base64.StdEncoding.EncodeToString(encrypted.GetPassword())
	return s.upsertMinerCredentialStrings(ctx, device, orgID, encodedUsername, encodedPassword, "save fleet node credentials")
}

func (s *Service) upsertMinerCredentialStrings(ctx context.Context, device *pairingpb.Device, orgID int64, username, password, operation string) error {
	if err := s.deviceStore.UpsertMinerCredentials(ctx, device, orgID, username, secrets.NewText(password)); err != nil {
		return fleeterror.LogInternal(component, operation, clientErrPair, err)
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
