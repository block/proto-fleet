package discovery

import (
	"fmt"
	"slices"
	"testing"

	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoveryranges"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
	"github.com/stretchr/testify/require"
)

func TestPartitionRequests_KeepsHostnameListsInBoundedCommands(t *testing.T) {
	scope, err := discoveryranges.Parse([]string{"10.0.0.0/24"})
	require.NoError(t, err)
	for _, tc := range []struct {
		name           string
		count, maximum int
		ports          []string
	}{
		{name: "failed and valid names stay together", count: 2, maximum: 256, ports: []string{"4028"}},
		{name: "address budget", count: 300, maximum: 256, ports: []string{"4028"}},
		{name: "default port budget", count: 300, maximum: 102},
	} {
		t.Run(tc.name, func(t *testing.T) {
			names := []string{"missing.lan", "valid.lan"}
			for len(names) < tc.count {
				names = append(names, fmt.Sprintf("miner-%d.lan", len(names)))
			}
			tasks, err := partitionRequests(ipListReq(names, tc.ports), scope)
			require.NoError(t, err)
			commands := slices.Collect(tasks)
			require.Equal(t, (tc.count+tc.maximum-1)/tc.maximum, len(commands))
			var received []string
			for _, command := range commands {
				require.NoError(t, ValidateRequest(command))
				require.LessOrEqual(t, len(command.GetIpList().GetIpAddresses()), tc.maximum)
				received = append(received, command.GetIpList().GetIpAddresses()...)
			}
			require.Equal(t, names, received)
		})
	}
}

func TestPartitionRequests_PreservesDirectProbingAndBudgets(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ports []string
		max   int
	}{
		{"one port", []string{"4028"}, 256},
		{"default ports", nil, 102},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope, err := discoveryranges.Parse([]string{"10.0.0.100-10.0.2.0", "10.0.2.10"})
			require.NoError(t, err)
			for _, list := range []bool{false, true} {
				t.Run(fmt.Sprint("list=", list), func(t *testing.T) {
					req := ipRangeReq("10.0.0.0", "10.0.2.20", tc.ports)
					if list {
						target, err := netscan.Range("10.0.0.0", "10.0.2.20")
						require.NoError(t, err)
						var addresses []string
						for address := range target.Addresses() {
							addresses = append(addresses, address.String())
						}
						req = ipListReq(addresses, tc.ports)
					}
					tasks, err := partitionRequests(req, scope)
					require.NoError(t, err)
					seen := map[string]bool{}
					for task := range tasks {
						require.Nil(t, task.GetNetworkScan())
						require.NoError(t, ValidateRequest(task))
						var addresses []string
						if list {
							addresses = task.GetIpList().GetIpAddresses()
						} else {
							target, err := netscan.Range(task.GetIpRange().GetStartIp(), task.GetIpRange().GetEndIp())
							require.NoError(t, err)
							for address := range target.Addresses() {
								addresses = append(addresses, address.String())
							}
						}
						require.LessOrEqual(t, len(addresses), tc.max)
						for _, address := range addresses {
							require.True(t, scope.ContainsString(address), "must not fill the gap between configured ranges")
							require.False(t, seen[address], "must not probe an address twice")
							seen[address] = true
						}
					}
					require.Len(t, seen, 414)
				})
			}
		})
	}
}

func TestPartitionRequests_ClipsBroadManualTargetsBeforeCommandLimits(t *testing.T) {
	scope, err := discoveryranges.Parse([]string{"10.0.10.100-10.0.11.150"})
	require.NoError(t, err)
	for _, req := range []*pairingpb.DiscoverRequest{
		networkScanReq("10.0.0.0/16", nil),
		ipRangeReq("10.0.0.0", "10.0.200.0", nil),
	} {
		t.Run(fmt.Sprintf("%T", req.GetMode()), func(t *testing.T) {
			require.NoError(t, ValidateRequest(req))
			tasks, err := partitionRequests(req, scope)
			require.NoError(t, err)
			count := 0
			for task := range tasks {
				require.NoError(t, ValidateRequest(task))
				var target netscan.Target
				if task.GetNetworkScan() != nil {
					target, err = netscan.ParseTarget(task.GetNetworkScan().GetTarget())
				} else {
					target, err = netscan.Range(task.GetIpRange().GetStartIp(), task.GetIpRange().GetEndIp())
				}
				require.NoError(t, err)
				require.LessOrEqual(t, target.Count(), uint64(102))
				for address := range target.Addresses() {
					require.True(t, scope.Contains(address))
					count++
				}
			}
			require.Equal(t, 307, count)
		})
	}
}
