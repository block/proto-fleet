package ct1

import (
	"context"
	"fmt"
	"slices"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Verify stored emptiness and the runtime of every selected physical board.
// A primary's empty diagnostics do not certify boards observed only by its peer.
func (d *Device) verifyClearedGroup(ctx context.Context, r sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	cleared := false
	out := sdk.MiningGroupResult{PoolsCleared: &cleared}
	selected, _, err := d.probeControllers(ctx, false)
	if err != nil {
		return out, err
	}
	client := mdk.NewMiningGroupServiceClient(selected.client.conn)
	before, err := observeGroups(ctx, client)
	if err != nil {
		return out, err
	}
	if !before.Enabled || before.Suspended || before.Pending {
		return out, nil
	}
	var group *sdk.MiningGroup
	for i := range before.Groups {
		if before.Groups[i].ID == r.GroupID {
			group = &before.Groups[i]
			break
		}
	}
	if group == nil {
		return out, nil
	}
	for _, mac := range r.MACs {
		if !slices.Contains(group.MACs, mac) {
			return out, nil
		}
	}
	observed := map[string]bool{}
	runtimeEmpty := true
	for peer := selected; peer != nil; peer = peer.standby {
		diagnostics, err := mdk.NewMiningGroupServiceClient(peer.client.conn).GetMiningGroupPoolDiagnostics(ctx, &mdk.GetMiningGroupPoolDiagnosticsRequest{GroupId: r.GroupID})
		if err != nil {
			if status.Code(err) == codes.Unimplemented {
				return out, sdk.NewErrUnsupportedCapability(sdk.CapabilityMiningGroupPoolClear)
			}
			return out, fmt.Errorf("group runtime pool verification unavailable")
		}
		// Preflight must establish support on every controller even while pools
		// are configured; otherwise an unsupported peer is found after clearing.
		if len(diagnostics.Pools) != 0 || diagnostics.WeightedMode {
			runtimeEmpty = false
		}
		for _, mac := range diagnostics.ObservedMacAddresses {
			observed[mac] = true
		}
	}
	for _, mac := range r.MACs {
		if !observed[mac] {
			return out, fmt.Errorf("selected board runtime pool verification unavailable")
		}
	}
	if !runtimeEmpty || group.Mining || len(group.Pools) != 0 {
		return out, nil
	}
	after, err := observeGroups(ctx, client)
	if err != nil {
		return out, err
	}
	if !after.Enabled || after.Suspended || after.Pending {
		return out, nil
	}
	for _, fresh := range after.Groups {
		if fresh.ID == group.ID && fresh.Revision == group.Revision && !fresh.Mining && len(fresh.Pools) == 0 && slices.Equal(fresh.MACs, group.MACs) {
			cleared = true
		}
	}
	return out, nil
}
