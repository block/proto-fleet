package command

import (
	"context"

	"google.golang.org/protobuf/proto"

	fleetmanagementpb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
)

// DeviceIdentifierResolver resolves a fleetmanagement DeviceSelector (whose
// all_devices case carries the rich MinerListFilter) into concrete device
// identifiers. It is satisfied by the fleetmanagement domain service and
// injected post-construction (that service depends on this one, so it cannot
// be passed to NewService — see SetDeviceIdentifierResolver).
type DeviceIdentifierResolver interface {
	ResolveDeviceIdentifiers(ctx context.Context, selector *fleetmanagementpb.DeviceSelector, orgID int64) ([]string, error)
}

// commandEligiblePairingStatuses mirrors pairingStatusValuesForSelector: when a
// caller-supplied filter names no pairing status, command dispatch targets the
// command-eligible set (paired plus default-password miners). The rich filter
// resolver otherwise defaults to PAIRED-only, which would silently drop
// default-password miners that the thin-selector path has always included.
var commandEligiblePairingStatuses = []fleetmanagementpb.PairingStatus{
	fleetmanagementpb.PairingStatus_PAIRING_STATUS_PAIRED,
	fleetmanagementpb.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
}

// fleetSelectorForMatchingFilter wraps a MinerListFilter (from the minercommand
// all_matching_filter selector case) in a fleetmanagement DeviceSelector,
// applying the command-eligible pairing default when the filter is unscoped on
// pairing status. The input proto is cloned so the caller's message is never
// mutated.
func fleetSelectorForMatchingFilter(filter *fleetmanagementpb.MinerListFilter) *fleetmanagementpb.DeviceSelector {
	resolved := &fleetmanagementpb.MinerListFilter{}
	if filter != nil {
		if cloned, ok := proto.Clone(filter).(*fleetmanagementpb.MinerListFilter); ok {
			resolved = cloned
		}
	}
	if len(resolved.PairingStatuses) == 0 {
		resolved.PairingStatuses = append([]fleetmanagementpb.PairingStatus(nil), commandEligiblePairingStatuses...)
	}
	return &fleetmanagementpb.DeviceSelector{
		SelectionType: &fleetmanagementpb.DeviceSelector_AllDevices{AllDevices: resolved},
	}
}
