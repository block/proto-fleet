package ct1

import (
	"context"
	"fmt"
	"sort"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (d *Device) MiningGroups(ctx context.Context, r sdk.MiningGroupRequest) (out sdk.MiningGroupResult, resultErr error) {
	defer func() {
		if !r.ReadOnly() && status.Code(resultErr) == codes.Aborted {
			out = sdk.MiningGroupResult{RevisionConflict: true}
			resultErr = nil
		}
	}()
	if err := r.Validate(); err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := mdk.NewMiningGroupServiceClient(d.client.conn)
	if r.Action == "observe" {
		return observeGroups(ctx, client)
	}
	var group *mdk.MiningGroupRecord
	switch r.Action {
	case "mining_status":
		v, e := mdk.NewMiningServiceClient(d.client.conn).GetMiningStatus(ctx, &mdk.GetMiningStatusRequest{})
		if e != nil {
			return out, e
		}
		out.MiningStatus = &sdk.MiningGroupMiningStatus{Mining: v.Mining, ActiveBoards: v.ActiveBoards}
	case "stop_legacy":
		capability, e := client.GetMiningGroupCapability(ctx, &mdk.GetMiningGroupCapabilityRequest{})
		if e != nil {
			return out, e
		}
		if capability.GetCapability() == nil {
			return out, fmt.Errorf("missing mining group capability")
		}
		// A lost stop acknowledgement may be retried after external activation.
		// Never turn that retry into a stop of the newly enabled default group.
		if !capability.Capability.Enabled {
			if _, e = mdk.NewMiningServiceClient(d.client.conn).StopMining(ctx, &mdk.StopMiningRequest{}); e != nil {
				return out, e
			}
		}
		out.StopRequested = true
	case "operation":
		v, e := client.GetMiningGroupOperation(ctx, &mdk.GetMiningGroupOperationRequest{Id: r.GroupID})
		if e != nil {
			return out, e
		}
		out.Operation = groupOperation(v.Operation)
	case "remove":
		_, e := client.DeleteMiningGroup(ctx, &mdk.DeleteMiningGroupRequest{Id: r.GroupID, ExpectedRevision: r.Revision, IdempotencyKey: r.Key})
		if e != nil {
			return out, e
		}
		out.Removed = true
	case "activate":
		v, e := client.ActivateMiningGroups(ctx, &mdk.ActivateMiningGroupsRequest{ExpectedStateRevision: r.Revision, IdempotencyKey: r.Key})
		if e != nil {
			return out, e
		}
		out.Enabled = v.GetCapability().GetEnabled()
	case "create":
		v, e := client.CreateMiningGroup(ctx, &mdk.CreateMiningGroupRequest{Metadata: &mdk.MiningGroupMetadata{Name: "Fleet " + r.ConfigID, ExternalRef: &r.ConfigID}, IdempotencyKey: r.Key})
		if e != nil {
			return out, e
		}
		group = v.Group
	case "replace_pools":
		pools := make([]*mdk.MiningGroupPoolInput, 0, len(r.Pools))
		for i, p := range r.Pools {
			password := p.Password
			pools = append(pools, &mdk.MiningGroupPoolInput{Pool: &mdk.MiningGroupPoolRecord{Id: []int32{1, 2, 3}[i], Name: fmt.Sprintf("Pool %d", i+1), Url: p.URL, Username: p.Username, Priority: []int32{0, 1, 2}[i], Enabled: true}, Password: &password})
		}
		v, e := client.ReplaceMiningGroupPools(ctx, &mdk.ReplaceMiningGroupPoolsRequest{GroupId: r.GroupID, ExpectedRevision: r.Revision, IdempotencyKey: r.Key, Pools: pools})
		if e != nil {
			return out, e
		}
		group = v.Group
	case "start":
		v, e := client.UpdateMiningGroupIntent(ctx, &mdk.UpdateMiningGroupIntentRequest{Id: r.GroupID, ExpectedRevision: r.Revision, IdempotencyKey: r.Key, DesiredMining: true, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"desired_mining"}}})
		if e != nil {
			return out, e
		}
		group = v.Group
	case "transfer":
		v, e := client.RequestMiningGroupTransfer(ctx, &mdk.RequestMiningGroupTransferRequest{SourceGroupId: r.SourceID, DestinationGroupId: r.GroupID, SourceExpectedRevision: r.SourceRevision, DestinationExpectedRevision: r.Revision, IdempotencyKey: r.Key, MacAddresses: r.MACs})
		if e != nil {
			return out, e
		}
		out.Operation = groupOperation(v.Operation)
	}
	if group != nil {
		g := groupRecord(group)
		out.Group = &g
	}
	return out, nil
}
func groupRecord(g *mdk.MiningGroupRecord) sdk.MiningGroup {
	return sdk.MiningGroup{ID: g.Id, Name: g.Name, Revision: g.Revision, Mining: g.DesiredMining, CustomPolicy: g.RequestedBoardWatts != nil || (g.PllAlgorithm != "" && g.PllAlgorithm != "NONE" && g.PllAlgorithm != "PLL_ALGORITHM_NONE")}
}
func groupOperation(o *mdk.MiningGroupOperationRecord) *sdk.MiningGroupOperation {
	if o == nil {
		return nil
	}
	r := &sdk.MiningGroupOperation{ID: o.Id, Phase: o.Phase}
	for _, b := range o.StopBlockers {
		r.BlockedMACs = append(r.BlockedMACs, b.MacAddress)
	}
	return r
}
func observeGroups(ctx context.Context, c mdk.MiningGroupServiceClient) (sdk.MiningGroupResult, error) {
	var out sdk.MiningGroupResult
	capability, e := c.GetMiningGroupCapability(ctx, &mdk.GetMiningGroupCapabilityRequest{})
	if status.Code(e) == codes.Unimplemented {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	v := capability.GetCapability()
	if v == nil || v.Name != "mining-groups-v1" || v.Version != 1 || v.AssignmentGranularity != "PHYSICAL_HASHBOARD" {
		return out, nil
	}
	out = sdk.MiningGroupResult{Supported: true, Enabled: v.Enabled, Suspended: v.Suspended, Revision: v.StateRevision, MaxGroups: int(v.MaxGroups)}
	if !v.Enabled {
		out.ObservedAt = time.Now().UTC()
		return out, nil
	}
	before, e := c.ListMiningGroups(ctx, &mdk.ListMiningGroupsRequest{})
	if e != nil {
		return out, e
	}
	members, e := c.ListMiningGroupMemberships(ctx, &mdk.ListMiningGroupMembershipsRequest{})
	if e != nil {
		return out, e
	}
	index := map[string]int{}
	for _, g := range before.Groups {
		index[g.Id] = len(out.Groups)
		converted := groupRecord(g)
		pools, e := c.ListMiningGroupPools(ctx, &mdk.ListMiningGroupPoolsRequest{GroupId: g.Id})
		if e != nil {
			return out, e
		}
		for _, p := range pools.Pools {
			converted.Pools = append(converted.Pools, sdk.MiningGroupPool{ID: p.Id, Name: p.Name, URL: p.Url, Username: p.Username, Priority: p.Priority, Enabled: p.Enabled, ShareWeight: p.ShareWeight})
		}
		sort.Slice(converted.Pools, func(i, j int) bool { return converted.Pools[i].Priority < converted.Pools[j].Priority })
		out.Groups = append(out.Groups, converted)
	}
	seen := map[string]bool{}
	for _, m := range members.Memberships {
		i, ok := index[m.GroupId]
		if !ok || seen[m.MacAddress] {
			return out, fmt.Errorf("inconsistent controller membership")
		}
		seen[m.MacAddress] = true
		out.Groups[i].MACs = append(out.Groups[i].MACs, m.MacAddress)
	}
	// Each membership/pool write advances its group's revision. A second read
	// prevents publishing a mixture observed across a concurrent mutation.
	after, e := c.ListMiningGroups(ctx, &mdk.ListMiningGroupsRequest{})
	if e != nil {
		return out, e
	}
	capAfter, e := c.GetMiningGroupCapability(ctx, &mdk.GetMiningGroupCapabilityRequest{})
	if e != nil {
		return out, e
	}
	if !proto.Equal(before, after) || !proto.Equal(capability, capAfter) {
		return sdk.MiningGroupResult{}, fmt.Errorf("mining groups changed during observation; retry")
	}
	out.Pending = len(before.PendingOperations) > 0
	out.ObservedAt = time.Now().UTC()
	for i := range out.Groups {
		sort.Strings(out.Groups[i].MACs)
	}
	return out, nil
}
