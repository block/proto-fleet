package discovery

import (
	"fmt"
	"iter"
	"testing"

	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
	"github.com/block/proto-fleet/server/internal/domain/discoveryranges"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
	"github.com/stretchr/testify/require"
)

func collectPartitions(tasks iter.Seq2[*pairingpb.DiscoverRequest, int]) (commands []*pairingpb.DiscoverRequest, probes []int) {
	for command, count := range tasks {
		commands = append(commands, command)
		probes = append(probes, count)
	}
	return commands, probes
}

func sum(values []int) (total int) {
	for _, value := range values {
		total += value
	}
	return total
}

func networkScanAddresses(t *testing.T, commands []*pairingpb.DiscoverRequest) []string {
	t.Helper()
	var addresses []string
	for _, command := range commands {
		target, err := netscan.ParseTarget(command.GetNetworkScan().GetTarget())
		require.NoError(t, err)
		for address := range target.Addresses() {
			addresses = append(addresses, address.String())
		}
	}
	return addresses
}

func TestPartitionRequests_KeepsHostnameListsInBoundedCommands(t *testing.T) {
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
			// Arrange
			scope, err := discoveryranges.Parse([]string{"10.0.0.0/24"})
			require.NoError(t, err)
			names := []string{"missing.lan", "valid.lan"}
			for len(names) < tc.count {
				names = append(names, fmt.Sprintf("miner-%d.lan", len(names)))
			}

			// Act
			tasks, err := partitionRequests(ipListReq(names, tc.ports), scope)
			require.NoError(t, err)
			commands, probes := collectPartitions(tasks)

			// Assert
			require.Equal(t, (tc.count+tc.maximum-1)/tc.maximum, len(commands))
			portCount := len(tc.ports)
			if portCount == 0 {
				portCount = discoverylimits.MaxPortsPerIP
			}
			var received []string
			for i, command := range commands {
				require.NoError(t, ValidateRequest(command))
				require.LessOrEqual(t, len(command.GetIpList().GetIpAddresses()), tc.maximum)
				require.Equal(t, len(command.GetIpList().GetIpAddresses())*portCount, probes[i])
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
			for _, list := range []bool{false, true} {
				t.Run(fmt.Sprint("list=", list), func(t *testing.T) {
					// Arrange
					scope, err := discoveryranges.Parse([]string{"10.0.0.100-10.0.2.0", "10.0.2.10"})
					require.NoError(t, err)
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

					// Act
					tasks, err := partitionRequests(req, scope)
					require.NoError(t, err)
					commands, probes := collectPartitions(tasks)

					// Assert
					seen := map[string]bool{}
					for i, task := range commands {
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
						require.LessOrEqual(t, probes[i], discoverylimits.MaxPartitionProbes)
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
	for _, req := range []*pairingpb.DiscoverRequest{
		networkScanReq("10.0.0.0/16", nil),
		ipRangeReq("10.0.0.0", "10.0.200.0", nil),
	} {
		t.Run(fmt.Sprintf("%T", req.GetMode()), func(t *testing.T) {
			// Arrange
			scope, err := discoveryranges.Parse([]string{"10.0.10.100-10.0.11.150"})
			require.NoError(t, err)
			require.NoError(t, ValidateRequest(req))

			// Act
			tasks, err := partitionRequests(req, scope)
			require.NoError(t, err)
			commands, _ := collectPartitions(tasks)

			// Assert
			count := 0
			for _, task := range commands {
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

func TestPartitionRequests_ClipsCIDRBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		wantFirst string
		wantLast  string
		wantCount int
	}{
		{name: "/24 excludes network and broadcast addresses", target: "10.0.0.0/24", wantFirst: "10.0.0.1", wantLast: "10.0.0.254", wantCount: 254},
		{name: "/30 excludes network and broadcast addresses", target: "10.0.0.4/30", wantFirst: "10.0.0.5", wantLast: "10.0.0.6", wantCount: 2},
		{name: "/31 keeps both addresses", target: "10.0.0.4/31", wantFirst: "10.0.0.4", wantLast: "10.0.0.5", wantCount: 2},
		{name: "/32 keeps the single address", target: "10.0.0.7/32", wantFirst: "10.0.0.7", wantLast: "10.0.0.7", wantCount: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			scope, err := discoveryranges.Parse([]string{"10.0.0.0/8"})
			require.NoError(t, err)

			// Act
			tasks, err := partitionRequests(networkScanReq(tt.target, []string{"4028"}), scope)
			require.NoError(t, err)
			commands, probes := collectPartitions(tasks)

			// Assert
			addresses := networkScanAddresses(t, commands)
			require.Len(t, addresses, tt.wantCount)
			require.Equal(t, tt.wantFirst, addresses[0])
			require.Equal(t, tt.wantLast, addresses[len(addresses)-1])
			require.Equal(t, tt.wantCount, sum(probes), "one probe per address and port")
		})
	}
}

func TestPartitionRequests_NetworkScanHostnameStaysUnresolved(t *testing.T) {
	// Arrange
	scope, err := discoveryranges.Parse([]string{"10.0.0.0/24"})
	require.NoError(t, err)

	// Act
	tasks, err := partitionRequests(networkScanReq("miner.lan", []string{"4028", "80"}), scope)
	require.NoError(t, err)
	commands, probes := collectPartitions(tasks)

	// Assert
	require.Len(t, commands, 1)
	require.Equal(t, "miner.lan", commands[0].GetNetworkScan().GetTarget())
	require.Equal(t, []string{"4028", "80"}, commands[0].GetNetworkScan().GetPorts())
	require.Equal(t, []int{2}, probes)
}
