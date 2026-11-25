package foremanimport

import (
	"fmt"
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

// normalizeForemanData normalizes equipment metadata only. Foreman cannot supply
// complete pool credentials, so observed pools never become Fleet configurations.
// Existing miner settings and Fleet configuration assignments remain untouched.
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

	for _, m := range miners {
		// Leave WorkerName unset: Foreman reports full pool usernames, not explicit Fleet worker names.
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

		importMiners = append(importMiners, im)
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
		Groups: importGroups,
		Racks:  importRacks,
	}
}
