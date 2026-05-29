package fleetnodepairing

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodeenrollment"
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
)

type Store interface {
	PairDeviceToFleetNode(ctx context.Context, fleetNodeID, deviceID, orgID int64, assignedBy *int64) (int64, error)
	UnpairDevice(ctx context.Context, deviceID, orgID int64) (int64, error)
	ListFleetNodeDevices(ctx context.Context, orgID int64, fleetNodeID *int64) ([]FleetNodeDevice, error)
	UpsertDiscoveredDeviceFromFleetNode(ctx context.Context, orgID int64, fleetNodeID int64, report DiscoveredDeviceReport) (int64, error)
	DeviceExistsInOrg(ctx context.Context, deviceID, orgID int64) (bool, error)
}

type Service struct {
	store           Store
	enrollmentStore fleetnodeenrollment.AgentStore
	transactor      stores.Transactor
}

func NewService(store Store, enrollmentStore fleetnodeenrollment.AgentStore, transactor stores.Transactor) *Service {
	return &Service{store: store, enrollmentStore: enrollmentStore, transactor: transactor}
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
		if node.EnrollmentStatus != fleetnodeenrollment.FleetNodeStatusConfirmed {
			return fleeterror.NewFailedPreconditionError("fleet node is not confirmed; cannot pair until enrollment completes")
		}
		rows, pairErr := s.store.PairDeviceToFleetNode(ctx, fleetNodeID, deviceID, orgID, assignedBy)
		if pairErr != nil {
			return fleeterror.LogInternal(component, "pair device", clientErrPair, pairErr)
		}
		if rows == 0 {
			return fleeterror.NewFailedPreconditionError("device already paired; unpair first")
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

// UpsertDiscoveredDevices validates the whole batch up front, then runs
// every upsert inside a single transaction so a mid-batch failure can't
// leave a committed prefix. Ownership-rejected rows (0 rows affected) are
// counted in rejectedOwnership without aborting the tx — they're the
// store's normal "we refused to overwrite a hijacked row" signal.
func (s *Service) UpsertDiscoveredDevices(ctx context.Context, fleetNodeID, orgID int64, reports []DiscoveredDeviceReport) (accepted, rejectedOwnership int64, err error) {
	for i, r := range reports {
		if vErr := validateReport(r); vErr != nil {
			return 0, 0, fleeterror.NewInvalidArgumentErrorf("report %d: %v", i, vErr)
		}
	}
	if txErr := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		for _, r := range reports {
			rows, upErr := s.store.UpsertDiscoveredDeviceFromFleetNode(ctx, orgID, fleetNodeID, r)
			if upErr != nil {
				return fleeterror.LogInternal(component, "upsert discovered device", clientErrUpsertDiscoveredDevice, upErr)
			}
			if rows == 0 {
				rejectedOwnership++
				continue
			}
			accepted++
		}
		return nil
	}); txErr != nil {
		return 0, 0, txErr
	}
	return accepted, rejectedOwnership, nil
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
	// URL scheme is bounded by proto-validate (max_len 32). The gateway
	// deliberately matches the source schema's permissive stance rather
	// than gating on a fixed allowlist — plugins like the virtual driver
	// emit non-http schemes, and the cloud-facing pairing.Device proto
	// accepts any string.
	return nil
}
