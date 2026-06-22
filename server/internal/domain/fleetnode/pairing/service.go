package pairing

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"

	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	component                          = "fleet node pairing"
	clientErrPair                      = "device pairing failed"
	clientErrUnpair                    = "device unpairing failed"
	clientErrList                      = "failed to list fleet node devices"
	clientErrUpsertDiscoveredDevice    = "discovery upsert failed"
	clientErrLookupDeviceForPairing    = "device lookup failed"
	clientErrLookupFleetNodeForPairing = "fleet node lookup failed"
	statusPaired                       = "PAIRED"
	statusAuthenticationNeeded         = "AUTHENTICATION_NEEDED"
	statusFailed                       = "FAILED"
)

type Store interface {
	DeviceIDByIdentifier(ctx context.Context, deviceIdentifier string, orgID int64) (int64, error)
	PairDeviceToFleetNode(ctx context.Context, fleetNodeID, deviceID, orgID int64, assignedBy *int64) (int64, error)
	TransferDiscoveredDeviceAttribution(ctx context.Context, fleetNodeID, deviceID, orgID int64) (int64, error)
	DeviceHasActiveCloudPairing(ctx context.Context, deviceID, orgID int64) (bool, error)
	UnpairDevice(ctx context.Context, deviceID, orgID int64) (int64, error)
	ListFleetNodeDevices(ctx context.Context, orgID int64, fleetNodeID *int64) ([]FleetNodeDevice, error)
	ListFleetNodeDiscoveredDevices(ctx context.Context, orgID int64, fleetNodeID, cursorID, limit *int64) ([]FleetNodeDiscoveredDevice, error)
	UpsertDiscoveredDeviceFromFleetNode(ctx context.Context, orgID int64, fleetNodeID int64, report DiscoveredDeviceReport) (int64, error)
	DeviceExistsInOrg(ctx context.Context, deviceID, orgID int64) (bool, error)
}

type Service struct {
	store           Store
	enrollmentStore enrollment.AgentStore
	transactor      stores.Transactor
	deviceStore     stores.DeviceStore
}

func NewService(store Store, enrollmentStore enrollment.AgentStore, transactor stores.Transactor, deviceStore ...stores.DeviceStore) *Service {
	s := &Service{store: store, enrollmentStore: enrollmentStore, transactor: transactor}
	if len(deviceStore) > 0 {
		s.deviceStore = deviceStore[0]
	}
	return s
}

func (s *Service) PairDevice(ctx context.Context, fleetNodeID, deviceID, orgID int64, assignedBy *int64) error {
	exists, err := s.store.DeviceExistsInOrg(ctx, deviceID, orgID)
	if err != nil {
		return fleeterror.LogInternal(component, "lookup device", clientErrLookupDeviceForPairing, err)
	}
	if !exists {
		return fleeterror.NewNotFoundError("device not found")
	}
	return s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		// Lock-and-recheck inside the TX so a concurrent revoke
		// can't soft-delete the node between the status check and
		// the INSERT. Matches the lock order Confirm/Revoke use.
		node, lockErr := s.enrollmentStore.LockFleetNodeByID(ctx, fleetNodeID, orgID)
		if lockErr != nil {
			if fleeterror.IsNotFoundError(lockErr) {
				return fleeterror.NewNotFoundError("fleet node not found")
			}
			return fleeterror.LogInternal(component, "lock fleet node", clientErrLookupFleetNodeForPairing, lockErr)
		}
		if node.EnrollmentStatus != enrollment.FleetNodeStatusConfirmed {
			return fleeterror.NewFailedPreconditionError("fleet node is not confirmed; cannot pair until enrollment completes")
		}
		// Refuse a device the cloud actively dials (device_pairing PAIRED): the
		// discovery upsert guard blocks refreshing a cloud-paired row, so pairing
		// it here would leave the node unable to refresh while the API reports it
		// as fleet-node paired. Operator must unpair from the cloud first.
		if cloudPaired, cloudErr := s.store.DeviceHasActiveCloudPairing(ctx, deviceID, orgID); cloudErr != nil {
			return fleeterror.LogInternal(component, "check cloud pairing", clientErrPair, cloudErr)
		} else if cloudPaired {
			return fleeterror.NewFailedPreconditionError("device is cloud-paired; unpair it from the cloud before pairing to a fleet node")
		}
		rows, pairErr := s.store.PairDeviceToFleetNode(ctx, fleetNodeID, deviceID, orgID, assignedBy)
		if pairErr != nil {
			return fleeterror.LogInternal(component, "pair device", clientErrPair, pairErr)
		}
		if rows == 0 {
			return fleeterror.NewFailedPreconditionError("device already paired; unpair first")
		}
		// Make the paired node the discovery owner so its future reports refresh
		// the row instead of being rejected by the upsert's attribution guard
		// (e.g. after replacing a revoked node). No-op for devices with no
		// discovered_device origin.
		if _, attrErr := s.store.TransferDiscoveredDeviceAttribution(ctx, fleetNodeID, deviceID, orgID); attrErr != nil {
			return fleeterror.LogInternal(component, "transfer discovery attribution", clientErrPair, attrErr)
		}
		return nil
	})
}

func (s *Service) UnpairDevice(ctx context.Context, deviceID, orgID int64) error {
	if _, err := s.store.UnpairDevice(ctx, deviceID, orgID); err != nil {
		return fleeterror.LogInternal(component, "unpair device", clientErrUnpair, err)
	}
	return nil
}

func (s *Service) ListPairs(ctx context.Context, orgID int64) ([]FleetNodeDevice, error) {
	pairs, err := s.store.ListFleetNodeDevices(ctx, orgID, nil)
	if err != nil {
		return nil, fleeterror.LogInternal(component, "list pairs", clientErrList, err)
	}
	return pairs, nil
}

func (s *Service) ListDevicesForFleetNode(ctx context.Context, fleetNodeID, orgID int64) ([]FleetNodeDevice, error) {
	pairs, err := s.store.ListFleetNodeDevices(ctx, orgID, &fleetNodeID)
	if err != nil {
		return nil, fleeterror.LogInternal(component, "list pairs for fleet node", clientErrList, err)
	}
	return pairs, nil
}

// ListDiscoveredDevicesForFleetNode lists fleet-node-discovered devices not yet
// paired to their node. A nil fleetNodeID returns all such devices in the org.
// Results page by ascending id: pass the previous nextCursor as cursorID and a
// positive limit. A nil limit returns every candidate (the pairing batch path
// needs the full set). nextCursor is non-nil only when a full page was returned
// and more rows may remain.
func (s *Service) ListDiscoveredDevicesForFleetNode(ctx context.Context, orgID int64, fleetNodeID, cursorID, limit *int64) ([]FleetNodeDiscoveredDevice, *int64, error) {
	devices, err := s.store.ListFleetNodeDiscoveredDevices(ctx, orgID, fleetNodeID, cursorID, limit)
	if err != nil {
		return nil, nil, fleeterror.LogInternal(component, "list discovered devices", clientErrList, err)
	}
	var nextCursor *int64
	if limit != nil && *limit > 0 && int64(len(devices)) == *limit {
		last := devices[len(devices)-1].ID
		nextCursor = &last
	}
	return devices, nextCursor, nil
}

func (s *Service) PairTargetsForDiscoveredDevices(ctx context.Context, orgID, fleetNodeID int64, deviceIdentifiers []string, pairAll bool) ([]*pairingpb.FleetNodePairTarget, error) {
	if fleetNodeID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("fleet_node_id is required")
	}
	if !pairAll && len(deviceIdentifiers) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("device_identifiers must not be empty unless pair_all_unpaired is true")
	}

	devices, _, err := s.ListDiscoveredDevicesForFleetNode(ctx, orgID, &fleetNodeID, nil, nil)
	if err != nil {
		return nil, err
	}
	byIdentifier := make(map[string]FleetNodeDiscoveredDevice, len(devices))
	for _, d := range devices {
		byIdentifier[d.DeviceIdentifier] = d
	}

	selected := devices
	if !pairAll {
		selected = make([]FleetNodeDiscoveredDevice, 0, len(deviceIdentifiers))
		seen := make(map[string]struct{}, len(deviceIdentifiers))
		for _, id := range deviceIdentifiers {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			d, ok := byIdentifier[id]
			if !ok {
				return nil, fleeterror.NewInvalidArgumentErrorf("device_identifier %q is not available for pairing on this fleet node", id)
			}
			selected = append(selected, d)
		}
	}
	if len(selected) == 0 {
		return nil, fleeterror.NewFailedPreconditionError("no unpaired discovered devices are available for pairing")
	}

	targets := make([]*pairingpb.FleetNodePairTarget, 0, len(selected))
	for _, d := range selected {
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
	return targets, nil
}

func (s *Service) ApplyPairResults(ctx context.Context, fleetNodeID, orgID int64, results []*gatewaypb.FleetNodePairResult) (accepted []*gatewaypb.FleetNodePairResult, rejected int64, err error) {
	if s.deviceStore == nil {
		return nil, 0, fleeterror.NewInternalError("fleet node pairing device store is not configured")
	}
	devices, _, err := s.ListDiscoveredDevicesForFleetNode(ctx, orgID, &fleetNodeID, nil, nil)
	if err != nil {
		return nil, 0, err
	}
	candidates := make(map[string]FleetNodeDiscoveredDevice, len(devices))
	for _, d := range devices {
		candidates[d.DeviceIdentifier] = d
	}

	accepted = make([]*gatewaypb.FleetNodePairResult, 0, len(results))
	for _, res := range results {
		if res == nil {
			rejected++
			continue
		}
		candidate, ok := candidates[res.GetDeviceIdentifier()]
		if !ok {
			rejected++
			continue
		}
		if err := s.applyPairResult(ctx, fleetNodeID, orgID, candidate, res); err != nil {
			if errors.Is(err, errPairResultRejected) {
				rejected++
				continue
			}
			return nil, 0, err
		}
		accepted = append(accepted, res)
	}
	return accepted, rejected, nil
}

var errPairResultRejected = errors.New("pair result rejected")

func (s *Service) applyPairResult(ctx context.Context, fleetNodeID, orgID int64, candidate FleetNodeDiscoveredDevice, res *gatewaypb.FleetNodePairResult) error {
	status, attachToNode, ok := statusForPairOutcome(res.GetOutcome())
	if !ok {
		return errPairResultRejected
	}
	device := deviceFromPairResult(candidate, res)
	return s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		if err := s.upsertPairResultDevice(ctx, orgID, device); err != nil {
			return err
		}
		if err := s.deviceStore.UpsertDevicePairing(ctx, device, orgID, status); err != nil {
			return fleeterror.LogInternal(component, "upsert pair result status", clientErrPair, err)
		}
		if !attachToNode {
			return nil
		}
		deviceID, err := s.store.DeviceIDByIdentifier(ctx, device.GetDeviceIdentifier(), orgID)
		if err != nil {
			return fleeterror.LogInternal(component, "lookup pair result device id", clientErrPair, err)
		}
		rows, err := s.store.PairDeviceToFleetNode(ctx, fleetNodeID, deviceID, orgID, nil)
		if err != nil {
			return fleeterror.LogInternal(component, "pair reported device", clientErrPair, err)
		}
		if rows == 0 {
			return errPairResultRejected
		}
		if _, attrErr := s.store.TransferDiscoveredDeviceAttribution(ctx, fleetNodeID, deviceID, orgID); attrErr != nil {
			return fleeterror.LogInternal(component, "transfer pair result attribution", clientErrPair, attrErr)
		}
		return nil
	})
}

func (s *Service) upsertPairResultDevice(ctx context.Context, orgID int64, device *pairingpb.Device) error {
	existing, err := s.deviceStore.GetDeviceByDeviceIdentifier(ctx, device.GetDeviceIdentifier(), orgID)
	if err != nil {
		if !fleeterror.IsNotFoundError(err) {
			return fleeterror.LogInternal(component, "lookup pair result device", clientErrPair, err)
		}
		if err := s.deviceStore.InsertDevice(ctx, device, orgID, device.GetDeviceIdentifier()); err != nil {
			return fleeterror.LogInternal(component, "insert pair result device", clientErrPair, err)
		}
		return nil
	}
	if device.GetMacAddress() == "" && device.GetSerialNumber() == "" {
		return nil
	}
	if device.GetMacAddress() == "" {
		device.MacAddress = existing.GetMacAddress()
	}
	if device.GetSerialNumber() == "" {
		device.SerialNumber = existing.GetSerialNumber()
	}
	if err := s.deviceStore.UpdateDeviceInfo(ctx, device, orgID); err != nil {
		return fleeterror.LogInternal(component, "update pair result device", clientErrPair, err)
	}
	return nil
}

func statusForPairOutcome(outcome gatewaypb.PairOutcome) (status string, attachToNode bool, ok bool) {
	switch outcome {
	case gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED:
		return statusPaired, true, true
	case gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED:
		return statusAuthenticationNeeded, false, true
	case gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_FAILED, gatewaypb.PairOutcome_PAIR_OUTCOME_ERROR:
		return statusFailed, false, true
	default:
		return "", false, false
	}
}

func deviceFromPairResult(candidate FleetNodeDiscoveredDevice, res *gatewaypb.FleetNodePairResult) *pairingpb.Device {
	return &pairingpb.Device{
		DeviceIdentifier: candidate.DeviceIdentifier,
		IpAddress:        candidate.IPAddress,
		Port:             candidate.Port,
		UrlScheme:        candidate.URLScheme,
		DriverName:       candidate.DriverName,
		Model:            coalesce(res.GetModel(), candidate.Model),
		Manufacturer:     coalesce(res.GetManufacturer(), candidate.Manufacturer),
		FirmwareVersion:  coalesce(res.GetFirmwareVersion(), candidate.FirmwareVersion),
		SerialNumber:     res.GetSerialNumber(),
		MacAddress:       res.GetMacAddress(),
	}
}

func PairingStatusForOutcome(outcome gatewaypb.PairOutcome) fm.PairingStatus {
	switch outcome {
	case gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED:
		return fm.PairingStatus_PAIRING_STATUS_PAIRED
	case gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED:
		return fm.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED
	case gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_FAILED, gatewaypb.PairOutcome_PAIR_OUTCOME_ERROR:
		return fm.PairingStatus_PAIRING_STATUS_FAILED
	default:
		return fm.PairingStatus_PAIRING_STATUS_UNSPECIFIED
	}
}

func coalesce(first, fallback string) string {
	if first != "" {
		return first
	}
	return fallback
}

// UpsertDiscoveredDevices validates the whole batch up front, then runs
// every upsert inside a single transaction so a mid-batch failure can't
// leave a committed prefix. Ownership-rejected rows (0 rows affected) are
// counted in rejectedOwnership without aborting the tx — they're the
// store's normal "we refused to overwrite a hijacked row" signal.
// Returns the indices into reports the store actually accepted so the
// caller can forward only those rows to operator-facing consumers.
func (s *Service) UpsertDiscoveredDevices(ctx context.Context, fleetNodeID, orgID int64, reports []DiscoveredDeviceReport) (acceptedIdx []int, rejectedOwnership int64, err error) {
	for i, r := range reports {
		if vErr := validateReport(r); vErr != nil {
			return nil, 0, fleeterror.NewInvalidArgumentErrorf("report %d: %v", i, vErr)
		}
	}
	// RunInTx may re-run this closure on a retryable failure, so tally into
	// locals reset on each entry; accumulating onto the named returns would
	// double-count a retried batch.
	var (
		accepted []int
		rejected int64
	)
	if txErr := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		accepted = accepted[:0]
		rejected = 0
		for i, r := range reports {
			rows, upErr := s.store.UpsertDiscoveredDeviceFromFleetNode(ctx, orgID, fleetNodeID, r)
			if upErr != nil {
				return fleeterror.LogInternal(component, "upsert discovered device", clientErrUpsertDiscoveredDevice, upErr)
			}
			if rows == 0 {
				rejected++
				continue
			}
			accepted = append(accepted, i)
		}
		return nil
	}); txErr != nil {
		return nil, 0, txErr
	}
	return accepted, rejected, nil
}

func validateReport(r DiscoveredDeviceReport) error {
	if r.DeviceIdentifier == "" {
		return fmt.Errorf("device_identifier is required")
	}
	addr, err := netip.ParseAddr(r.IPAddress)
	if err != nil {
		return fmt.Errorf("ip_address %q is not a valid address", r.IPAddress)
	}
	// First line of defense; cloud never dials these IPs directly.
	if !addr.IsPrivate() {
		return fmt.Errorf("ip_address %q is not in a private range (RFC1918/RFC4193)", r.IPAddress)
	}
	port, err := strconv.Atoi(r.Port)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("port %q is not in 1-65535", r.Port)
	}
	// url_scheme is untrusted agent input that later becomes a clickable
	// scheme://ip link in the operator UI. Require the RFC 3986 scheme grammar
	// (not an allowlist — plugins legitimately emit non-http schemes like
	// stratum+tcp) so an injection payload such as "javascript:alert(1)//"
	// can't be stored. The clickable web URL is separately restricted to
	// http/https at construction (constructWebViewURL).
	if r.URLScheme != "" && !urlSchemeRE.MatchString(r.URLScheme) {
		return fmt.Errorf("url_scheme %q is not a valid scheme", r.URLScheme)
	}
	return nil
}

// urlSchemeRE is the RFC 3986 scheme grammar: ALPHA *( ALPHA / DIGIT / "+" / "-" / "." ).
var urlSchemeRE = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*$`)
