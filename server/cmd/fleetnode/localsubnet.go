package main

import (
	"errors"
	"fmt"

	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
)

// errNoLocalSubnet means the host has no usable IPv4 subnet to scan for a
// LocalSubnetTarget command. Surfaces as AGENT_INCAPABLE so a fan-out skips this
// node and tries the others.
var errNoLocalSubnet = errors.New("no local IPv4 subnet found")

// detectLocalSubnets returns the subnet(s) the agent scans for a local-subnet
// nmap command (the nmaptarget.LocalSubnetTarget sentinel).
//
// For parity with combined mode it reuses the server's own primary-interface
// detection (networking.GetLocalNetworkInfo) — the same logic the cloud Discover
// path scans. This is intentionally less robust than per-NIC private filtering
// (it picks one interface, doesn't skip virtual/container NICs, and doesn't
// narrow or cap the mask); hardening is a follow-up. The localSubnets seam lets
// tests inject canned CIDRs.
func (r *RunCmd) detectLocalSubnets() ([]string, error) {
	if r.localSubnets != nil {
		return r.localSubnets()
	}
	info, err := networking.GetLocalNetworkInfo()
	if err != nil {
		return nil, fmt.Errorf("get local network info: %w", err)
	}
	if info.Subnet == "" {
		return nil, errNoLocalSubnet
	}
	return []string{info.Subnet}, nil
}
