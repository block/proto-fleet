package foremanimport

import (
	"fmt"
	"net"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/fleetimport"
	"github.com/block/proto-fleet/server/internal/infrastructure/foreman"
)

// parseModel extracts the model from a Foreman type name by stripping
// the first word (manufacturer prefix) and any firmware suffix in parentheses.
// Example: "Antminer S21 Pro (vnish)" → "S21 Pro"
func parseModel(typeName string) string {
	parts := strings.Fields(typeName)
	if len(parts) <= 1 {
		return typeName
	}

	// Drop the first word (manufacturer prefix)
	rest := strings.Join(parts[1:], " ")

	// Strip parenthesized firmware suffix: "(vnish)", "(200T)", etc.
	if idx := strings.LastIndex(rest, "("); idx > 0 {
		rest = strings.TrimSpace(rest[:idx])
	}

	if rest == "" {
		return typeName
	}
	return rest
}

// poolNameFromURL generates a human-readable pool name from a stratum URL.
// Examples: "mine.ocean.xyz:3334" → "mine.ocean.xyz"
//
//	"stratum+tcp://ca.stratum.braiins.com:3333" → "ca.stratum.braiins.com"
func poolNameFromURL(url string) string {
	// Strip scheme (stratum+tcp://, stratum+ssl://, etc.)
	if idx := strings.Index(url, "://"); idx >= 0 {
		url = url[idx+3:]
	}
	// Strip port using net.SplitHostPort (handles IPv6 correctly)
	if host, _, err := net.SplitHostPort(url); err == nil {
		return host
	}
	return url
}

// basePoolUsername extracts the fleet-level username from a Foreman worker string
// by splitting on the first dot. "wallet.worker1" → "wallet", "wallet" → "wallet".
func basePoolUsername(worker string) string {
	if idx := strings.Index(worker, "."); idx > 0 {
		return worker[:idx]
	}
	return worker
}

// workerNameFromPool extracts the per-miner worker name from a Foreman worker string.
// "wallet.worker1" → "worker1", "wallet" → "".
func workerNameFromPool(worker string) string {
	if idx := strings.Index(worker, "."); idx > 0 && idx < len(worker)-1 {
		return worker[idx+1:]
	}
	return ""
}

// normalizeForemanData converts Foreman-specific types to the source-agnostic import format.
func normalizeForemanData(
	miners []foreman.Miner,
	groups []foreman.SiteMapGroup,
	racks []foreman.SiteMapRack,
) *fleetimport.ImportData {
	groupByID := make(map[int]foreman.SiteMapGroup)
	for _, g := range groups {
		groupByID[g.ID] = g
	}

	importMiners := make([]fleetimport.ImportMiner, 0, len(miners))
	seenPools := make(map[string]bool)
	var importPools []fleetimport.ImportPool

	for _, m := range miners {
		im := fleetimport.ImportMiner{
			SourceID: fmt.Sprintf("%d", m.ID),
			IP:       m.IP,
			MAC:      m.MAC,
			Name:     m.Name,
			Model:    parseModel(m.Type.Name),
		}
		if m.Location != nil {
			im.RackID = fmt.Sprintf("%d", m.Location.RackID)
			im.Row = int32(m.Location.Row)      //nolint:gosec // Foreman rack positions are small integers
			im.Column = int32(m.Location.Index) //nolint:gosec // Foreman rack positions are small integers
		}

		// Extract worker name from the first non-devfee pool
		for _, pool := range m.Pools {
			if pool.URL == "devfee" || pool.URL == "" || pool.Worker == "" {
				continue
			}
			im.WorkerName = workerNameFromPool(pool.Worker)
			break
		}

		importMiners = append(importMiners, im)

		for _, pool := range m.Pools {
			if pool.URL == "devfee" || pool.URL == "" {
				continue
			}
			username := basePoolUsername(pool.Worker)
			key := pool.URL + "|" + username
			if seenPools[key] {
				continue
			}
			seenPools[key] = true
			importPools = append(importPools, fleetimport.ImportPool{
				URL:      pool.URL,
				Username: username,
				Name:     poolNameFromURL(pool.URL),
			})
		}
	}

	importGroups := make([]fleetimport.ImportGroup, 0, len(groups))
	for _, g := range groups {
		importGroups = append(importGroups, fleetimport.ImportGroup{
			SourceID: fmt.Sprintf("%d", g.ID),
			Name:     g.Name,
		})
	}

	importRacks := make([]fleetimport.ImportRack, 0, len(racks))
	for _, r := range racks {
		ir := fleetimport.ImportRack{
			SourceID: fmt.Sprintf("%d", r.ID),
			Name:     r.Name,
		}
		if r.GroupID != nil {
			ir.GroupID = fmt.Sprintf("%d", *r.GroupID)
			if parentGroup, ok := groupByID[*r.GroupID]; ok {
				ir.Location = parentGroup.Name
			}
		}
		importRacks = append(importRacks, ir)
	}

	return &fleetimport.ImportData{
		Miners: importMiners,
		Pools:  importPools,
		Groups: importGroups,
		Racks:  importRacks,
	}
}
