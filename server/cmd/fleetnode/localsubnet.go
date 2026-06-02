package main

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// autoSubnetMinPrefixBits caps a detected subnet at /22 (<=1024 hosts) — the
// same ceiling the manual nmap path enforces (nmaptarget.MinIPv4PrefixBits). A
// NIC configured with a wider mask (e.g. /16) is narrowed around its own host
// address so the scan stays bounded and finishes inside the command timeout.
const autoSubnetMinPrefixBits = 22

// maxAutoSubnets caps how many distinct subnets one command scans, so a
// multi-homed host with many interfaces can't fan one command into a huge sweep.
const maxAutoSubnets = 8

// errNoLocalPrivateSubnet means no connected, non-virtual interface had a private
// IPv4 address — the agent has nothing to scan. Surfaces as AGENT_INCAPABLE so a
// fan-out skips this node and tries the others.
var errNoLocalPrivateSubnet = errors.New("no connected private IPv4 subnet found")

// virtualIfacePrefixes are name prefixes for container/VPN/virtual adapters whose
// subnets aren't the miner LAN. Best-effort: a miss only means a virtual private
// subnet might be scanned (still port-probed, still private), never a public scan.
var virtualIfacePrefixes = []string{
	"docker", "br-", "veth", "virbr", "vmnet", "vboxnet",
	"tun", "tap", "utun", "cni", "cali", "flannel", "kube",
	"zt", "tailscale", "ts", "wg",
}

// detectLocalSubnets returns the private IPv4 subnet(s) the agent should scan for
// a local-subnet nmap command (the nmaptarget.LocalSubnetTarget sentinel). The
// localSubnets seam lets tests inject canned CIDRs; production enumerates the
// host's interfaces.
func (r *RunCmd) detectLocalSubnets() ([]string, error) {
	if r.localSubnets != nil {
		return r.localSubnets()
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}
	return selectLocalPrivateSubnets(ifaces, (*net.Interface).Addrs)
}

// selectLocalPrivateSubnets returns the canonical CIDR(s) of the connected,
// non-virtual, private IPv4 subnet(s) of the given interfaces. addrsOf is
// injected for testing ((*net.Interface).Addrs in production). Subnets wider than
// /22 are narrowed around the host address, results are deduped and capped at
// maxAutoSubnets, and IPv6 is ignored (the manual nmap path rejects IPv6 CIDR
// too). Returns errNoLocalPrivateSubnet when none qualify.
func selectLocalPrivateSubnets(ifaces []net.Interface, addrsOf func(*net.Interface) ([]net.Addr, error)) ([]string, error) {
	seen := make(map[string]struct{})
	out := make([]string, 0, maxAutoSubnets)
	for i := range ifaces {
		iface := ifaces[i]
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagRunning == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 || isVirtualIface(iface.Name) {
			continue
		}
		addrs, err := addrsOf(&iface)
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			addr, ok := netip.AddrFromSlice(ipNet.IP)
			if !ok {
				continue
			}
			addr = addr.Unmap()
			if !addr.Is4() || !addr.IsPrivate() {
				continue
			}
			ones, _ := ipNet.Mask.Size()
			if ones <= 0 || ones > addr.BitLen() {
				continue // non-canonical mask
			}
			if ones < autoSubnetMinPrefixBits {
				ones = autoSubnetMinPrefixBits
			}
			cidr := netip.PrefixFrom(addr, ones).Masked().String()
			if _, dup := seen[cidr]; dup {
				continue
			}
			seen[cidr] = struct{}{}
			out = append(out, cidr)
			if len(out) >= maxAutoSubnets {
				return out, nil
			}
		}
	}
	if len(out) == 0 {
		return nil, errNoLocalPrivateSubnet
	}
	return out, nil
}

func isVirtualIface(name string) bool {
	lower := strings.ToLower(name)
	for _, p := range virtualIfacePrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}
