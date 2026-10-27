// Package mininggroups reconciles Fleet's board pool intent with controller groups.
package mininggroups

import (
	"fmt"
	"slices"
)

type StepKind string

const (
	Create    StepKind = "create"
	Remove    StepKind = "remove"
	Configure StepKind = "configure"
	Start     StepKind = "start"
	Transfer  StepKind = "transfer"
	Wait      StepKind = "wait"
	Done      StepKind = "done"
)

// ConfigRevision identifies a Fleet configuration, never a hash of credentials.
// Managed and Configured come from durable Fleet application receipts, not names.
type Group struct {
	ID             string   `json:"id"`
	Revision       int64    `json:"revision"`
	ConfigRevision string   `json:"config_revision"`
	Managed        bool     `json:"managed"`
	Configured     bool     `json:"configured"`
	Mining         bool     `json:"mining"`
	CustomPolicy   bool     `json:"custom_policy"`
	Members        []string `json:"members"`
}
type Snapshot struct {
	Enabled   bool    `json:"enabled"`
	MaxGroups int     `json:"max_groups"`
	Pending   bool    `json:"pending"`
	Groups    []Group `json:"groups"`
}

// An unsupported assignment requires a new request or an operator policy
// change. Retrying it indefinitely cannot make progress on this snapshot.
type unsupportedAssignmentError struct{ message string }

func (e *unsupportedAssignmentError) Error() string { return e.message }

// Step is persisted before I/O. Its exact request and key are replayed after a
// lost response; a new observation can produce a new step only after resolution.
type Step struct {
	Kind                   StepKind `json:"kind"`
	GroupID                string   `json:"group_id,omitempty"`
	SourceID               string   `json:"source_id,omitempty"`
	ExpectedRevision       int64    `json:"expected_revision,omitempty"`
	SourceExpectedRevision int64    `json:"source_expected_revision,omitempty"`
	ConfigRevision         string   `json:"config_revision,omitempty"`
	MACs                   []string `json:"macs,omitempty"`
}

// Plan returns one minimal next mutation. Unselected members are preserved.
// Equal pool contents never merge independently owned configuration identities.
func Plan(desired map[string]string, observed Snapshot) (Step, error) {
	if !observed.Enabled {
		return Step{}, fmt.Errorf("enable mining groups before setting board pools")
	}
	if observed.Pending {
		return Step{Kind: Wait}, nil
	}
	membership := map[string]Group{}
	groups := slices.Clone(observed.Groups)
	slices.SortFunc(groups, func(a, b Group) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	for _, g := range groups {
		for _, mac := range g.Members {
			if _, exists := membership[mac]; exists {
				return Step{}, fmt.Errorf("ambiguous board membership")
			}
			membership[mac] = g
		}
	}
	keys := make([]string, 0, len(desired))
	for mac, key := range desired {
		if mac == "" || key == "" {
			return Step{}, fmt.Errorf("board and configuration identity are required")
		}
		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		var destination *Group
		for i := range groups {
			g := &groups[i]
			if g.Managed && g.ID != "default" && g.ConfigRevision == key {
				destination = g
				break
			}
		}
		if destination == nil {
			// Reconfigure in place only when every member requests the same revision.
			for i := range groups {
				g := &groups[i]
				if !g.Managed || g.ID == "default" || len(g.Members) == 0 {
					continue
				}
				all := true
				for _, mac := range g.Members {
					if desired[mac] != key {
						all = false
						break
					}
				}
				if all {
					return Step{Kind: Configure, GroupID: g.ID, ExpectedRevision: g.Revision, ConfigRevision: key}, nil
				}
			}
			// Reuse an empty Fleet-owned group whose previous configuration has no
			// remaining intent. Repeated edits must not consume controller capacity.
			for _, g := range groups {
				if g.Managed && g.ID != "default" && !g.CustomPolicy && len(g.Members) == 0 && !slices.Contains(keys, g.ConfigRevision) {
					return Step{Kind: Configure, GroupID: g.ID, ExpectedRevision: g.Revision, ConfigRevision: key}, nil
				}
			}
			for mac, want := range desired {
				if want == key && membership[mac].CustomPolicy {
					return Step{}, &unsupportedAssignmentError{"splitting a group with custom operator policy requires operator policy review"}
				}
			}
			if observed.MaxGroups <= 0 || len(groups) >= observed.MaxGroups {
				return Step{}, &unsupportedAssignmentError{"mining group capacity reached"}
			}
			return Step{Kind: Create, ConfigRevision: key}, nil
		}
		if !destination.Configured {
			return Step{Kind: Configure, GroupID: destination.ID, ExpectedRevision: destination.Revision, ConfigRevision: key}, nil
		}
		if !destination.Mining {
			return Step{Kind: Start, GroupID: destination.ID, ExpectedRevision: destination.Revision, ConfigRevision: key}, nil
		}
		macs := []string{}
		for mac, want := range desired {
			if want == key && membership[mac].ID != destination.ID {
				macs = append(macs, mac)
			}
		}
		slices.Sort(macs)
		if len(macs) == 0 {
			continue
		}
		source := membership[macs[0]]
		if source.CustomPolicy || destination.CustomPolicy {
			return Step{}, &unsupportedAssignmentError{"moving boards with custom operator policy requires operator policy review"}
		}
		selected := []string{}
		for _, mac := range macs {
			if membership[mac].ID == source.ID {
				selected = append(selected, mac)
			}
		}
		return Step{Kind: Transfer, SourceID: source.ID, GroupID: destination.ID, ExpectedRevision: destination.Revision, SourceExpectedRevision: source.Revision, MACs: selected, ConfigRevision: key}, nil
	}
	for _, g := range groups {
		if g.Managed && g.ID != "default" && len(g.Members) == 0 && !g.CustomPolicy && !slices.Contains(keys, g.ConfigRevision) {
			return Step{Kind: Remove, GroupID: g.ID, ExpectedRevision: g.Revision}, nil
		}
	}
	return Step{Kind: Done}, nil
}
