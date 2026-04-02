package deviceresolver

import (
	"context"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

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
	if list == nil || len(list.DeviceIdentifiers) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("include_devices requires at least one device identifier")
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
