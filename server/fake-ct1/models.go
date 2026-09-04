package main

import (
	"fmt"
	"math/rand/v2"
	"sync"
	"time"
)

// Topology and telemetry defaults for a simulated CT1 container.
//
// A full container is 96 modules x 3 hashboards = 288 boards. The tree is
// Tank -> Module -> Slot -> Hashboard. Counts are env-parametrizable (see
// ContainerConfig) so tests can shrink the 288-board tree.
const (
	defaultVersion        = "2.0.0"
	defaultTanks          = 2 // 2 tanks x 48 modules x 3 slots = 288 boards
	defaultModulesPerTank = 48
	defaultSlotsPerModule = 3
	defaultAsicsPerBoard  = 312

	product      = "CT1"
	manufacturer = "Proto"
	model        = "CT1-Immersion"
	asicModel    = "BM1370"

	// Per-board telemetry bases. A CT1 hydro board is far larger than an R2
	// rig; these are realistic-shaped, not physically exact.
	boardIdealHashrateTHS = 335.0
	boardPowerTargetW     = 5200.0
	boardPowerTargetMinW  = 3000.0
	boardPowerTargetMaxW  = 6000.0
	boardVoltage          = 14.2
	boardTempC            = 58.0
	asicTempC             = 66.0

	// ASIC layout for the per-board asics endpoint.
	asicStackCount   = 4
	asicFrequencyMHz = 525
	asicVoltageMv    = 385
	asicPassRateBp   = 9960 // basis points (0-10000) => 99.6%

	// Plant / PUE.
	basePUE           = 1.04
	plantInletTempC   = 34.0
	plantOutletTempC  = 48.0
	plantAmbientTempC = 27.0

	telemetryVariation = 0.03 // 3% random variation on reads
)

// Hashboard mining-state enum values (mdk.v2 MiningState).
const (
	miningStateMining       = "MINING_STATE_MINING"
	miningStateIdle         = "MINING_STATE_IDLE"
	miningStateDisconnected = "MINING_STATE_DISCONNECTED"
)

// ContainerConfig carries the topology parameters for a simulated container.
type ContainerConfig struct {
	InstanceID     string
	ServerID       string
	Hostname       string
	Version        string
	Tanks          int
	ModulesPerTank int
	SlotsPerModule int
	AsicsPerBoard  int
}

// ContainerState holds the full simulated container tree and shared metadata.
// All reads and writes go through mu.
type ContainerState struct {
	mu sync.RWMutex

	Config    ContainerConfig
	ServerID  string
	Version   string
	Hostname  string
	IPAddress string
	StartTime time.Time

	PerformanceMode  string
	BalanceBays      bool
	HashOnDisconnect bool

	Tanks   []*Tank
	Modules []*Module // flattened, in tree order

	boardsByMAC    map[string]*Hashboard
	boardsBySerial map[string]*Hashboard
	modulesByID    map[string]*Module
}

// Tank groups modules within the container.
type Tank struct {
	ID      int
	Modules []*Module
}

// Module is a serviceable unit holding a fixed number of hashboard slots.
type Module struct {
	ID       int    // per-tank module number, starting at 1
	ModuleID string // stable string identifier used by the module APIs
	TankID   int
	Slots    []*Slot

	// FixedMillivolts is the operator-pinned module voltage, 0 when unset.
	FixedMillivolts int64
	FixedVoltageSet bool
}

// Slot is a physical hashboard position, possibly empty.
type Slot struct {
	Index       int
	BreakerID   string
	PowerFeedID string
	Occupied    bool
	Board       *Hashboard
}

// Hashboard is a single mining board within a slot.
type Hashboard struct {
	MAC             string
	Serial          string
	HWRevision      string
	FirmwareVersion string
	FirmwareGitHash string
	ChipID          string

	State    string // one of the miningState* constants
	Isolated bool

	// Location.
	TankID      int
	ModuleNum   int
	ModuleID    string
	SlotIndex   int
	BreakerID   string
	PowerFeedID string

	// Power target (watts). ScopedWatts overrides the default when set.
	PowerTargetW    float64
	PowerTargetMinW float64
	PowerTargetMaxW float64
	ScopedWatts     float64
	ScopedWattsSet  bool

	StartTime time.Time
}

// NewContainerState builds the full container tree from cfg.
func NewContainerState(cfg ContainerConfig) *ContainerState {
	now := time.Now()
	s := &ContainerState{
		Config:          cfg,
		ServerID:        cfg.ServerID,
		Version:         cfg.Version,
		Hostname:        cfg.Hostname,
		IPAddress:       "127.0.0.1",
		StartTime:       now,
		PerformanceMode: "PERFORMANCE_MODE_MAXIMUM_HASHRATE",
		boardsByMAC:     make(map[string]*Hashboard),
		boardsBySerial:  make(map[string]*Hashboard),
		modulesByID:     make(map[string]*Module),
	}

	globalBoard := 0
	for t := 1; t <= cfg.Tanks; t++ {
		tank := &Tank{ID: t}
		for m := 1; m <= cfg.ModulesPerTank; m++ {
			moduleID := fmt.Sprintf("T%d-M%02d", t, m)
			module := &Module{ID: m, ModuleID: moduleID, TankID: t}
			for sl := range cfg.SlotsPerModule {
				globalBoard++
				board := newHashboard(cfg, t, m, moduleID, sl, globalBoard, now)
				slot := &Slot{
					Index:       sl,
					BreakerID:   fmt.Sprintf("BRK-%s-%d", moduleID, sl),
					PowerFeedID: fmt.Sprintf("PF-T%d-%02d", t, ((m-1)*cfg.SlotsPerModule+sl)/2+1),
					Occupied:    true,
					Board:       board,
				}
				board.BreakerID = slot.BreakerID
				board.PowerFeedID = slot.PowerFeedID
				module.Slots = append(module.Slots, slot)
				s.boardsByMAC[board.MAC] = board
				s.boardsBySerial[board.Serial] = board
			}
			tank.Modules = append(tank.Modules, module)
			s.Modules = append(s.Modules, module)
			s.modulesByID[moduleID] = module
		}
		s.Tanks = append(s.Tanks, tank)
	}
	return s
}

func newHashboard(cfg ContainerConfig, tank, moduleNum int, moduleID string, slot, globalBoard int, now time.Time) *Hashboard {
	return &Hashboard{
		MAC:             fmt.Sprintf("02:C1:00:%02X:%02X:%02X", (globalBoard>>16)&0xFF, (globalBoard>>8)&0xFF, globalBoard&0xFF),
		Serial:          fmt.Sprintf("CT1-%s-%04d", cfg.InstanceID, globalBoard),
		HWRevision:      "rev-c",
		FirmwareVersion: cfg.Version,
		FirmwareGitHash: "e87a5197",
		ChipID:          asicModel,
		State:           miningStateMining,
		TankID:          tank,
		ModuleNum:       moduleNum,
		ModuleID:        moduleID,
		SlotIndex:       slot,
		PowerTargetW:    boardPowerTargetW,
		PowerTargetMinW: boardPowerTargetMinW,
		PowerTargetMaxW: boardPowerTargetMaxW,
		StartTime:       now,
	}
}

// ModuleCount returns the total number of modules in the container.
func (s *ContainerState) ModuleCount() int { return len(s.Modules) }

// BoardCount returns the total number of hashboards in the container.
func (s *ContainerState) BoardCount() int { return len(s.boardsByMAC) }

// findBoard resolves a board by MAC address (CT1) or serial number (R2).
// Caller must hold at least a read lock.
func (s *ContainerState) findBoard(identifier string) *Hashboard {
	if b, ok := s.boardsByMAC[identifier]; ok {
		return b
	}
	return s.boardsBySerial[identifier]
}

// targetWatts returns the effective per-board power target.
func (b *Hashboard) targetWatts() float64 {
	if b.ScopedWattsSet {
		return b.ScopedWatts
	}
	return b.PowerTargetW
}

// isMining reports whether the board is actively hashing. Container-level
// "mining" is derived from board state (any board hashing) rather than a
// separate intent flag, so scoped and global start/stop can't contradict the
// reported status.
func (b *Hashboard) isMining() bool {
	return !b.Isolated && b.State == miningStateMining
}

// vary applies a small random variation (+/- telemetryVariation) to base so
// repeated telemetry reads look realistic, matching server/fake-proto-rig.
func vary(base float64) float64 {
	return base * (1 + (rand.Float64()*2-1)*telemetryVariation)
}
