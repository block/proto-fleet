// Package fleetimport provides source-agnostic fleet import logic.
// Data sources (Foreman API, CSV, etc.) normalize their data into these
// types, then call Importer to create pools, groups, and racks.
package fleetimport

// ImportData is the normalized input for a fleet import, regardless of source.
type ImportData struct {
	Miners []ImportMiner
	Pools  []ImportPool
	Groups []ImportGroup
	Racks  []ImportRack
}

// ImportMiner represents a miner to be discovered and paired.
type ImportMiner struct {
	SourceID   string // opaque ID from the source (e.g., Foreman miner ID, CSV row number)
	IP         string
	MAC        string
	Name       string
	Model      string
	RackID     string // source-specific rack ID this miner belongs to (empty if none)
	Row        int32  // position within rack
	Column     int32  // position within rack
	WorkerName string // per-miner worker name extracted from pool config (e.g., "worker1")
}

// ImportPool is a pool configuration to create.
type ImportPool struct {
	URL      string
	Username string
	Name     string
}

// ImportGroup is a group to create with its member miners.
type ImportGroup struct {
	SourceID string // opaque ID from the source
	Name     string
}

// ImportRack is a rack to create.
type ImportRack struct {
	SourceID string // opaque ID from the source
	Name     string
	Location string // physical location (e.g., parent group name)
	Rows     int32
	Columns  int32
	GroupID  string // source ID of the parent group (empty if none)
}

// PruneUnreferencedRacks removes racks that no miner references.
// Groups are not pruned since they may be explicitly requested even without rack references.
func (d *ImportData) PruneUnreferencedRacks() {
	referencedRacks := make(map[string]bool)
	for _, m := range d.Miners {
		if m.RackID != "" {
			referencedRacks[m.RackID] = true
		}
	}

	filtered := d.Racks[:0]
	for _, r := range d.Racks {
		if referencedRacks[r.SourceID] {
			filtered = append(filtered, r)
		}
	}
	d.Racks = filtered
}

// ImportResult summarizes what was created.
type ImportResult struct {
	PoolsCreated    int32
	GroupsCreated   int32
	RacksCreated    int32
	DevicesAssigned int32
	WorkerNamesSet  int32
	MinerNamesSet   int32
}
