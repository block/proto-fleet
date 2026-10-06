package deviceresolver

import (
	"context"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Caps for explicit device_list selectors. These mirror the buf.validate
// constraints that protected the deprecated `repeated string
// device_identifiers` field on the per-RPC requests
// (min_items: 1, max_items: 10000, items.string.{min_len: 1, max_len: 256}).
// common.v1.DeviceSelector.device_list does not carry those rules, so we
// enforce them in code at every entry point that unwraps the variant.
const (
	MaxDeviceIdentifiers   = 10000
	MaxDeviceIdentifierLen = 256
)

// ValidateDeviceIdentifiers enforces the per-list and per-identifier caps
// on an explicit device_list. Callers that unwrap
// common.v1.DeviceSelector.device_list themselves (e.g. the deviceset
// translate helpers) should call this before forwarding the list to the
// store layer; otherwise empty strings + oversized lists flow through to
// SQL where they either silently match no rows or amplify load.
func ValidateDeviceIdentifiers(ids []string) error {
	if len(ids) == 0 {
		return fleeterror.NewInvalidArgumentError("device_identifiers must not be empty")
	}
	if len(ids) > MaxDeviceIdentifiers {
		return fleeterror.NewInvalidArgumentErrorf("device_identifiers exceeds maximum of %d values", MaxDeviceIdentifiers)
	}
	for _, id := range ids {
		if id == "" {
			return fleeterror.NewInvalidArgumentError("device_identifier must not be empty")
		}
		if len(id) > MaxDeviceIdentifierLen {
			return fleeterror.NewInvalidArgumentErrorf("device_identifier exceeds maximum length of %d", MaxDeviceIdentifierLen)
		}
	}
	return nil
}

// DeviceOwnershipChecker is the subset of DeviceStore needed by the resolver.
type DeviceOwnershipChecker interface {
	AllDevicesBelongToOrg(ctx context.Context, deviceIdentifiers []string, orgID int64) (bool, error)
	GetDeviceIdentifiersByOrgWithFilter(ctx context.Context, orgID int64, filter *interfaces.MinerFilter) ([]string, error)
}

// Resolver resolves a common.v1.DeviceSelector into device identifiers,
// validating ownership for explicit device lists.
type Resolver struct {
	store DeviceOwnershipChecker
}

// New creates a Resolver backed by the given store.
func New(store DeviceOwnershipChecker) *Resolver {
	return &Resolver{store: store}
}

// Resolve resolves a common.v1.DeviceSelector into device identifiers for the given org.
func (r *Resolver) Resolve(ctx context.Context, selector *commonpb.DeviceSelector, orgID int64) ([]string, error) {
	if selector == nil {
		return nil, fleeterror.NewInvalidArgumentError("device_selector is required")
	}

	switch sel := selector.SelectionType.(type) {
	case *commonpb.DeviceSelector_DeviceList:
		return r.resolveExplicitDevices(ctx, sel.DeviceList, orgID)

	case *commonpb.DeviceSelector_AllDevices:
		return r.store.GetDeviceIdentifiersByOrgWithFilter(ctx, orgID, &interfaces.MinerFilter{})

	default:
		return nil, fleeterror.NewInvalidArgumentError("device_selector must specify a selection_type")
	}
}

// ResolveExplicitDevices validates and deduplicates an explicit device list, checking org ownership.
func (r *Resolver) ResolveExplicitDevices(ctx context.Context, list *commonpb.DeviceIdentifierList, orgID int64) ([]string, error) {
	return r.resolveExplicitDevices(ctx, list, orgID)
}

func (r *Resolver) resolveExplicitDevices(ctx context.Context, list *commonpb.DeviceIdentifierList, orgID int64) ([]string, error) {
	if list == nil {
		return nil, fleeterror.NewInvalidArgumentError("include_devices requires at least one device identifier")
	}
	if err := ValidateDeviceIdentifiers(list.DeviceIdentifiers); err != nil {
		return nil, err
	}
	ids := deduplicateStrings(list.DeviceIdentifiers)

	allBelong, err := r.store.AllDevicesBelongToOrg(ctx, ids, orgID)
	if err != nil {
		return nil, err
	}
	if !allBelong {
		return nil, fleeterror.NewForbiddenError("access denied to one or more requested devices")
	}
	return ids, nil
}

func deduplicateStrings(s []string) []string {
	seen := make(map[string]struct{}, len(s))
	result := make([]string, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}
	return result
}
