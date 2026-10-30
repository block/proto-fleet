package discovery

import (
	"fmt"
	"iter"
	"net/netip"
	"slices"

	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
	"github.com/block/proto-fleet/server/internal/domain/discoveryranges"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
	"google.golang.org/protobuf/proto"
)

// partitionRequests clips literal targets to the assigned ranges and lazily
// emits bounded commands. Hostnames stay unresolved until the node applies its
// own DNS and range policy. Network scans retain TCP probing; lists and ranges
// retain direct plugin discovery. Each command is paired with its address x port
// probe count, the most reports it can produce.
func partitionRequests(req *pairingpb.DiscoverRequest, assigned discoveryranges.Scope) (iter.Seq2[*pairingpb.DiscoverRequest, int], error) {
	var ports, literals, hostnames []string
	var requested discoveryranges.Scope
	network := req.GetNetworkScan() != nil
	list := req.GetIpList() != nil
	switch m := req.GetMode().(type) {
	case *pairingpb.DiscoverRequest_IpList:
		ports = m.IpList.GetPorts()
		for _, value := range m.IpList.GetIpAddresses() {
			if address, ok := parseScopeAddr(value); ok {
				if !address.Is4() {
					return nil, fleeterror.NewInvalidArgumentError("Fleet Node discovery ranges support only IPv4 targets")
				}
				literals = append(literals, address.String())
			} else {
				hostnames = append(hostnames, value)
			}
		}
		var err error
		requested, err = discoveryranges.Parse(literals)
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentError(err.Error())
		}
	case *pairingpb.DiscoverRequest_IpRange:
		ports = m.IpRange.GetPorts()
		target, err := netscan.Range(m.IpRange.GetStartIp(), m.IpRange.GetEndIp())
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentError(err.Error())
		}
		requested, err = discoveryranges.FromBounds(target.Bounds())
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentError(err.Error())
		}
	case *pairingpb.DiscoverRequest_NetworkScan:
		ports = m.NetworkScan.GetPorts()
		if ScansLocalSubnet(m.NetworkScan) {
			requested = assigned
			break
		}
		target, err := netscan.ParseTarget(m.NetworkScan.GetTarget())
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentError(err.Error())
		}
		if target.Count() == 0 {
			hostnames = append(hostnames, m.NetworkScan.GetTarget())
			break
		}
		requested, err = discoveryranges.FromBounds(target.Bounds())
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentError(err.Error())
		}
	default:
		return nil, fleeterror.NewInvalidArgumentError("unsupported Fleet Node discovery mode")
	}
	portCount := len(ports)
	if portCount == 0 {
		portCount = discoverylimits.MaxPortsPerIP
	}
	chunkSize := min(discoverylimits.MaxPartitionAddresses, discoverylimits.MaxPartitionProbes/portCount)
	scope := assigned.IntersectScope(requested)
	return func(yield func(*pairingpb.DiscoverRequest, int) bool) {
		for addresses := range scope.Chunks(chunkSize) {
			if list {
				if !yield(&pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_IpList{IpList: &pairingpb.IPListModeRequest{IpAddresses: addresses, Ports: ports}}}, len(addresses)*portCount) {
					return
				}
				continue
			}
			// The existing network-scan grammar expresses ranges only within one
			// /24. Separate disjoint runs as well, so a chunk never fills gaps.
			for start := 0; start < len(addresses); {
				first := netip.MustParseAddr(addresses[start])
				last := first
				end := start + 1
				for end < len(addresses) {
					next := netip.MustParseAddr(addresses[end])
					if next != last.Next() || (network && first.As4()[2] != next.As4()[2]) {
						break
					}
					last = next
					end++
				}
				var command *pairingpb.DiscoverRequest
				if network {
					target := first.String()
					if first != last {
						target = fmt.Sprintf("%s-%d", first, last.As4()[3])
					}
					command = &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_NetworkScan{NetworkScan: &pairingpb.NetworkScanModeRequest{Target: target, Ports: ports}}}
				} else {
					command = &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_IpRange{IpRange: &pairingpb.IPRangeModeRequest{StartIp: first.String(), EndIp: last.String(), Ports: ports}}}
				}
				if !yield(command, (end-start)*portCount) {
					return
				}
				start = end
			}
		}
		if list {
			for names := range slices.Chunk(hostnames, chunkSize) {
				command := proto.CloneOf(req)
				command.GetIpList().IpAddresses = names
				if !yield(command, len(names)*portCount) {
					return
				}
			}
			return
		}
		for _, hostname := range hostnames {
			command := proto.CloneOf(req)
			command.GetNetworkScan().Target = hostname
			if !yield(command, portCount) {
				return
			}
		}
	}, nil
}
