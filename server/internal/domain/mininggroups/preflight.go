package mininggroups

import (
	"fmt"
	"slices"
)

// ValidateAssignment checks the entire sequence, including transfers that follow
// group creation, pool configuration or start. It uses the production planner on
// a private projection; synthetic groups and mutations never reach a controller.
func ValidateAssignment(desired map[string]string, observed Snapshot) error {
	projected := observed
	projected.Groups = slices.Clone(observed.Groups)
	ids := map[string]bool{}
	for i, group := range projected.Groups {
		projected.Groups[i].Members = slices.Clone(group.Members)
		ids[group.ID] = true
	}
	nextID := 0
	// Each desired configuration needs at most create/configure/start, each
	// selected board at most one transfer, and each old group at most one removal.
	// Keep a bound so a future planner regression cannot hang submission.
	for range 5 * (len(desired) + len(observed.Groups) + 1) {
		step, err := Plan(desired, projected)
		if err != nil {
			return err
		}
		switch step.Kind {
		case Done:
			return nil
		case Wait:
			return fmt.Errorf("wait for the controller's pending transfer and refresh before assigning pools")
		case Create:
			id := ""
			for id == "" || ids[id] {
				id = fmt.Sprintf("preflight-%d", nextID)
				nextID++
			}
			ids[id] = true
			projected.Groups = append(projected.Groups, Group{ID: id, Revision: 1, Managed: true, ConfigRevision: step.ConfigRevision})
		case Remove:
			projected.Groups = slices.DeleteFunc(projected.Groups, func(g Group) bool { return g.ID == step.GroupID })
		case Configure, Start, Stop, Transfer:
			for i := range projected.Groups {
				group := &projected.Groups[i]
				if step.Kind == Transfer && group.ID == step.SourceID {
					group.Members = slices.DeleteFunc(group.Members, func(mac string) bool { return slices.Contains(step.MACs, mac) })
				}
				if group.ID != step.GroupID {
					continue
				}
				switch {
				case step.Kind == Configure:
					group.ConfigRevision = step.ConfigRevision
					group.Configured = true
				case step.Kind == Stop:
					group.Mining = false
				case step.Kind == Start:
					group.Mining = true
				case step.Kind == Transfer:
					group.Members = append(group.Members, step.MACs...)
				}
			}
		default:
			return fmt.Errorf("unsupported mining group planning step")
		}
	}
	return fmt.Errorf("mining group assignment could not be fully planned")
}
