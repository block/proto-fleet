package ct1

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (d *Device) MiningGroups(ctx context.Context, r sdk.MiningGroupRequest) (out sdk.MiningGroupResult, resultErr error) {
	write := !r.ReadOnly()
	resumeDefault := r.Action == "start" && r.GroupID == "default"
	defer func() {
		if write && !resumeDefault && status.Code(resultErr) == codes.Aborted {
			out = sdk.MiningGroupResult{RevisionConflict: true}
			resultErr = nil
		}
	}()
	if err := r.Validate(); err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if write {
		d.writeMu.Lock()
		defer d.writeMu.Unlock()
	}
	if r.Action == "verify_cleared" {
		return d.verifyClearedGroup(ctx, r)
	}
	c, err := d.selectClient(ctx, write)
	if err != nil {
		return out, err
	}
	client := mdk.NewMiningGroupServiceClient(c.conn)
	if r.Action == "observe" {
		return observeGroups(ctx, client)
	}
	var group *mdk.MiningGroupRecord
	switch r.Action {
	case sdk.MiningGroupReadDefault:
		before, err := readDefaultGroup(ctx, client)
		if err != nil {
			return out, err
		}
		legacy := mdk.NewPoolServiceClient(c.conn)
		pools, err := readWholePools(ctx, legacy)
		if err != nil {
			return out, err
		}
		if err := checkDefaultPools(ctx, legacy, pools); err != nil {
			return out, err
		}
		after, err := readDefaultGroup(ctx, client)
		if err != nil {
			return out, err
		}
		if !proto.Equal(before, after) {
			return out, fmt.Errorf("Default mining intent changed during observation; retry")
		}
		converted, err := defaultGroupWithPools(before, pools)
		if err != nil {
			return out, err
		}
		if len(r.DefaultURLSigningKey) != 0 {
			urls := make([]string, 0, len(pools))
			for _, pool := range pools {
				urls = append(urls, pool.Url)
			}
			encoded, err := json.Marshal(urls)
			if err != nil {
				return out, fmt.Errorf("encode Default pool URLs for review: %w", err)
			}
			mac := hmac.New(sha256.New, r.DefaultURLSigningKey)
			mac.Write(encoded)
			converted.DefaultURLFingerprint = fmt.Sprintf("%x", mac.Sum(nil))
		}
		out.Group = &converted
		return out, nil
	case "mining_status":
		v, e := c.mining.GetMiningStatus(ctx, &mdk.GetMiningStatusRequest{})
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
			if _, e = c.mining.StopMining(ctx, &mdk.StopMiningRequest{}); e != nil {
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
	case "deactivate":
		v, e := client.DeactivateMiningGroups(ctx, &mdk.DeactivateMiningGroupsRequest{ExpectedStateRevision: r.Revision, IdempotencyKey: r.Key})
		if e != nil {
			return out, e
		}
		out.Deactivated = v.GetCapability() != nil && !v.Capability.Enabled
	case "create":
		v, e := client.CreateMiningGroup(ctx, &mdk.CreateMiningGroupRequest{Metadata: &mdk.MiningGroupMetadata{Name: "Fleet " + r.ConfigID, ExternalRef: &r.ConfigID}, IdempotencyKey: r.Key})
		if e != nil {
			return out, e
		}
		group = v.Group
	case "replace_pools", "clear_pools":
		pools := make([]*mdk.MiningGroupPoolInput, 0, len(r.Pools))
		for i, p := range r.Pools {
			priority := [...]int32{0, 1, 2}[i]
			password := p.Password
			id := priority + 1
			name := fmt.Sprintf("Pool %d", i+1)
			if len(r.PoolIDs) != 0 {
				id = r.PoolIDs[i]
			}
			if len(r.PoolNames) != 0 {
				name = r.PoolNames[i]
			}
			pools = append(pools, &mdk.MiningGroupPoolInput{Pool: &mdk.MiningGroupPoolRecord{Id: id, Name: name, Url: p.URL, Username: p.Username, Priority: priority, Enabled: true}, Password: &password})
		}
		v, e := client.ReplaceMiningGroupPools(ctx, &mdk.ReplaceMiningGroupPoolsRequest{GroupId: r.GroupID, ExpectedRevision: r.Revision, IdempotencyKey: r.Key, Pools: pools})
		if e != nil {
			if r.Action == "replace_pools" && r.GroupID != "default" && len(r.PoolIDs) == 0 &&
				(status.Code(e) == codes.Unavailable || status.Code(e) == codes.InvalidArgument) &&
				legacyPoolIDsMissing(ctx, client, r) {
				return sdk.MiningGroupResult{RevisionConflict: true}, nil
			}
			return out, e
		}
		group = v.Group
	case "start", "stop":
		if resumeDefault {
			group, err = resumeDefaultMining(ctx, client, c.mining, r.Revision)
			if err != nil {
				return out, err
			}
			out.DefaultResumeSuperseded = !group.DesiredMining
			break
		}
		v, e := client.UpdateMiningGroupIntent(ctx, &mdk.UpdateMiningGroupIntentRequest{Id: r.GroupID, ExpectedRevision: r.Revision, IdempotencyKey: r.Key, DesiredMining: r.Action == "start", UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"desired_mining"}}})
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

// Replay the original request first. A stable expected revision and missing slot
// prove the replacement could not have committed and cannot still apply.
func legacyPoolIDsMissing(ctx context.Context, client mdk.MiningGroupServiceClient, r sdk.MiningGroupRequest) bool {
	request := &mdk.GetMiningGroupRequest{Id: r.GroupID}
	before, err := client.GetMiningGroup(ctx, request)
	if err != nil || before.GetGroup().GetId() != r.GroupID || before.GetGroup().GetRevision() != r.Revision {
		return false
	}
	pools, err := client.ListMiningGroupPools(ctx, &mdk.ListMiningGroupPoolsRequest{GroupId: r.GroupID})
	if err != nil {
		return false
	}
	after, err := client.GetMiningGroup(ctx, request)
	if err != nil || after.GetGroup().GetId() != r.GroupID || after.GetGroup().GetRevision() != r.Revision {
		return false
	}
	for i := range len(r.Pools) {
		if !slices.ContainsFunc(pools.GetPools(), func(pool *mdk.MiningGroupPoolRecord) bool { return int(pool.GetId()) == i+1 }) {
			return true
		}
	}
	return false
}

func readDefaultGroup(ctx context.Context, groups mdk.MiningGroupServiceClient) (*mdk.MiningGroupRecord, error) {
	response, err := groups.GetMiningGroup(ctx, &mdk.GetMiningGroupRequest{Id: "default"})
	if err != nil {
		return nil, fmt.Errorf("read Default mining intent: %w", err)
	}
	group := response.GetGroup()
	if group.GetId() != "default" || group.GetRevision() <= 0 {
		return nil, fmt.Errorf("invalid Default mining intent")
	}
	return group, nil
}

func resumeDefaultMining(ctx context.Context, groups mdk.MiningGroupServiceClient, mining mdk.MiningServiceClient, expectedRevision int64) (*mdk.MiningGroupRecord, error) {
	mode, err := groups.GetMiningGroupCapability(ctx, &mdk.GetMiningGroupCapabilityRequest{})
	if err != nil {
		return nil, fmt.Errorf("read mining group mode: %w", err)
	}
	if !mode.GetCapability().GetEnabled() || mode.GetCapability().GetSuspended() {
		return nil, status.Errorf(codes.FailedPrecondition, "mining groups must be enabled and active to restore Default mining")
	}
	current, err := readDefaultGroup(ctx, groups)
	if err != nil {
		return nil, err
	}
	if current.DesiredMining || current.Revision > expectedRevision {
		return current, nil
	}
	// A newer stopped intent can follow an unacknowledged start. Keep the saved
	// revision, since replanning from that state could override an operator stop.
	if current.Revision != expectedRevision {
		return nil, status.Errorf(codes.FailedPrecondition, "Default mining intent changed before resume")
	}
	if _, err := mining.StartMining(ctx, &mdk.StartMiningRequest{}); err != nil {
		return nil, fmt.Errorf("resume Default mining: %w", err)
	}
	current, err = readDefaultGroup(ctx, groups)
	if err != nil {
		return nil, err
	}
	if !current.DesiredMining && current.Revision <= expectedRevision {
		return nil, fmt.Errorf("Default mining resume was not confirmed")
	}
	return current, nil
}

func publicDefaultPoolURL(raw string) string {
	const shorthandScheme = "stratum+tcp://"
	endpoint := raw
	shorthand := !strings.Contains(raw, "://")
	if shorthand {
		endpoint = shorthandScheme + raw
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || u.Opaque != "" {
		return ""
	}
	if u.User == nil && (u.Path == "" || u.Path == "/") && u.RawQuery == "" && !u.ForceQuery && !strings.Contains(raw, "#") {
		return raw
	}
	if shorthand {
		return u.Host
	}
	return u.Scheme + "://" + u.Host
}

func defaultGroupWithPools(group *mdk.MiningGroupRecord, pools []*mdk.Pool) (sdk.MiningGroup, error) {
	converted := groupRecord(group)
	converted.Pools = make([]sdk.MiningGroupPool, 0, len(pools))
	for _, p := range pools {
		if p.Id <= 0 || p.Id > math.MaxInt32 {
			return sdk.MiningGroup{}, fmt.Errorf("invalid Default pool ID")
		}
		converted.Pools = append(converted.Pools, sdk.MiningGroupPool{ID: int32(p.Id), Name: p.Name, URL: publicDefaultPoolURL(p.Url), Username: p.Username, Priority: p.Priority, Enabled: p.Enabled, ShareWeight: p.ShareWeight})
	}
	return converted, nil
}

func checkDefaultPools(ctx context.Context, legacy mdk.PoolServiceClient, original []*mdk.Pool) error {
	latest, err := readWholePools(ctx, legacy)
	if err != nil {
		return err
	}
	if !slices.EqualFunc(original, latest, func(a, b *mdk.Pool) bool { return proto.Equal(a, b) }) {
		return fmt.Errorf("Default pools changed during observation; retry")
	}
	return nil
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
		if g.Id != "default" {
			pools, e := c.ListMiningGroupPools(ctx, &mdk.ListMiningGroupPoolsRequest{GroupId: g.Id})
			if e != nil {
				return out, e
			}
			for _, p := range pools.Pools {
				converted.Pools = append(converted.Pools, sdk.MiningGroupPool{ID: p.Id, Name: p.Name, URL: p.Url, Username: p.Username, Priority: p.Priority, Enabled: p.Enabled, ShareWeight: p.ShareWeight})
			}
		}
		slices.SortFunc(converted.Pools, func(a, b sdk.MiningGroupPool) int {
			return cmp.Or(cmp.Compare(a.Priority, b.Priority), cmp.Compare(a.ID, b.ID))
		})
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
	// A second read of the group and mode records detects other concurrent writes.
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
		slices.Sort(out.Groups[i].MACs)
	}
	return out, nil
}
