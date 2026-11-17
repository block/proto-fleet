package main

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
)

// Topology and telemetry defaults for a simulated CT1 container.
//
// A full container is 96 modules x 3 hashboards = 288 boards, matching a real
// CT1 (6 tanks). The tree is Tank -> Module -> Slot -> Hashboard. Counts are
// env-parametrizable (see ContainerConfig) so tests can shrink the 288-board tree.
const (
	defaultVersion        = "2.0.0"
	defaultTanks          = 6 // 6 tanks x 16 modules x 3 slots = 288 boards
	defaultModulesPerTank = 16
	defaultSlotsPerModule = 3
	defaultAsicsPerBoard  = 312

	product      = "CT1"
	manufacturer = "Proto"
	model        = "CT1-Immersion"
	asicModel    = "Proto Chip 3"

	// Per-board telemetry bases. A CT1 hydro board is far larger than an R2
	// rig; these are realistic-shaped, not physically exact.
	boardIdealHashrateTHS = 204.42
	boardPowerTargetW     = 3000.0
	boardPowerTargetMinW  = 2500.0
	boardPowerTargetMaxW  = 3000.0
	boardVoltage          = 28.0
	boardTempC            = 50.0
	asicTempC             = 60.0

	// ASIC layout for the per-board asics endpoint.
	asicStackCount   = 4
	asicFrequencyMHz = 525
	asicVoltageMv    = 385
	asicPassRateBp   = 9920 // basis points (0-10000) => 99.2%

	// Plant / PUE.
	plantInletTempC   = 32.0
	plantAmbientTempC = 25.0
)

// Hashboard mining-state enum values (mdk.v2 MiningState).
const (
	miningStateMining       = "MINING_STATE_MINING"
	miningStateIdle         = "MINING_STATE_IDLE"
	miningStateDisconnected = "MINING_STATE_DISCONNECTED"
	miningStateError        = "MINING_STATE_ERROR"
)

var containerSetpointDefaults = []containerSetpoint{
	{Name: "dry_cooler_auto_temp_c", Value: 32, Unit: "°C"},
	{Name: "coolant_pump_speed_hz", Value: 50, Unit: "Hz"},
	{Name: "dry_cooler_manual_speed_percent", Value: 80, Unit: "%"},
	{Name: "air_conditioner_1_temp_c", Value: 24, Unit: "°C"},
	{Name: "air_conditioner_2_temp_c", Value: 24, Unit: "°C"},
}

func (s *ContainerState) containerSetpoint(name string) containerSetpoint {
	for _, point := range containerSetpointDefaults {
		if point.Name == name {
			point.Value = s.ContainerSetpoints[name]
			return point
		}
	}
	return containerSetpoint{Name: name}
}

const (
	demoFaultTank              = 2
	demoLightFaultTank         = 3
	demoLightFaultModule       = 8
	demoInactiveTank           = 4
	demoInactiveSleepingModule = 4
	demoInactiveOfflineModule  = 9
	demoSleepingModule         = 3
	demoOfflineModule          = 6
	demoAttentionModule        = 11
)

// ContainerConfig carries the topology parameters for a simulated container.
type ContainerConfig struct {
	InstanceID        string
	IdentityNamespace string
	OccupiedBoards    int
	ResponseDelay     time.Duration
	ServerID          string
	Hostname          string
	Version           string
	Tanks             int
	ModulesPerTank    int
	SlotsPerModule    int
	AsicsPerBoard     int
	DemoModuleStates  bool
	Seed              int
	// UnlocatedBoards moves the first N boards into the map's inventory group;
	// -1 selects all boards. The underlying physical simulation stays intact.
	UnlocatedBoards int

	DemoAllBoardFaults bool
	// DemoPlantAlarms starts with plant alarms raised and the buzzer sounding.
	DemoPlantAlarms bool
	// AlarmResetUnsupported mimics real CT1 hardware, whose PLC clears alarm bits
	// only when their conditions clear. Zero value keeps reset supported.
	AlarmResetUnsupported bool
	// FanReverseUnsupported omits the dry-cooler fan reversal, as on Frigate
	// versions or PLC programs without it.
	FanReverseUnsupported bool
	// FanReverseMinute is the wall time of one simulated procedure minute.
	FanReverseMinute time.Duration
}

// ContainerState holds the full simulated container tree and shared metadata.
// All reads and writes go through mu.
type ContainerState struct {
	firmware *simulatedFirmware
	mu       sync.RWMutex
	// requests serializes API calls from every transport; see serve.
	requests     sync.Mutex
	metrics      operationMetrics
	now          func() time.Time
	sampleTime   time.Time
	observedAt   time.Time
	history      *simulationHistory
	miningGroups *simulatedMiningGroups
	pools        map[int64]simulatedPool
	// ha is non-nil when this controller is one member of a CT1_HA_PAIR.
	ha         *haInstance
	nextPoolID int64

	Config    ContainerConfig
	ServerID  string
	Version   string
	Hostname  string
	IPAddress string
	StartTime time.Time

	PerformanceMode    string
	BalanceBays        bool
	HashOnDisconnect   bool
	ContainerControls  map[string]bool
	ContainerSetpoints map[string]float64

	plantAlarms  []simulatedPlantAlarm
	buzzerActive bool
	fanReverse   simulatedFanReverse

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
	// Who last changed the voltage setting, and when; zero until first changed.
	VoltageUpdatedBy string
	VoltageUpdatedAt time.Time
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

	// Identify LED: the desired mode recorded by SetIdentifyLED and, for blink,
	// when the controller stops blinking on its own.
	IdentifyLED       mdk.HashboardIdentifyLEDMode
	IdentifyExpiresAt time.Time

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

	StartTime          time.Time
	StateChangedAt     time.Time
	PreviousState      string
	DisconnectedSample *boardSample
}

// NewContainerState builds the full container tree from cfg.
func NewContainerState(cfg ContainerConfig) *ContainerState {
	return newContainerState(cfg, time.Now().Truncate(5*time.Second))
}

func newContainerState(cfg ContainerConfig, now time.Time) *ContainerState {
	s := &ContainerState{
		Config:          cfg,
		now:             time.Now,
		sampleTime:      now,
		observedAt:      now,
		ServerID:        cfg.ServerID,
		Version:         cfg.Version,
		Hostname:        cfg.Hostname,
		IPAddress:       "127.0.0.1",
		StartTime:       now,
		PerformanceMode: "PERFORMANCE_MODE_MAXIMUM_HASHRATE",
		ContainerControls: map[string]bool{
			"air_conditioner_1": true,
			"air_conditioner_2": true,
			"coolant_pump":      true,
			"dry_cooler":        true,
			"dry_cooler_auto":   true,
			"tank_led_light":    true,
		},
		ContainerSetpoints: make(map[string]float64, len(containerSetpointDefaults)),
		boardsByMAC:        make(map[string]*Hashboard),
		boardsBySerial:     make(map[string]*Hashboard),
		modulesByID:        make(map[string]*Module),
		firmware:           newSimulatedFirmware(),
		fanReverse:         simulatedFanReverse{durationMinutes: fanReverseDefaultDuration},
	}
	for _, point := range containerSetpointDefaults {
		s.ContainerSetpoints[point.Name] = point.Value
	}
	if cfg.DemoPlantAlarms {
		s.plantAlarms = demoPlantAlarms()
		s.buzzerActive = true
	}

	s.initPools()
	globalBoard := 0
	for t := 1; t <= cfg.Tanks; t++ {
		tank := &Tank{ID: t}
		for m := 1; m <= cfg.ModulesPerTank; m++ {
			moduleID := fmt.Sprintf("T%d-M%02d", t, m)
			if cfg.IdentityNamespace != "" {
				moduleID = cfg.IdentityNamespace + ":" + moduleID
			}
			module := &Module{ID: m, ModuleID: moduleID, TankID: t}
			for sl := range cfg.SlotsPerModule {
				globalBoard++
				var board *Hashboard
				if cfg.OccupiedBoards == 0 || globalBoard <= cfg.OccupiedBoards {
					board = newHashboard(cfg, t, m, moduleID, sl, globalBoard, now)
				}
				slot := &Slot{
					Index:       sl,
					BreakerID:   fmt.Sprintf("BRK-%s-%d", moduleID, sl),
					PowerFeedID: fmt.Sprintf("pdu-%d-outlet-%02d", t, m),
					Occupied:    board != nil,
					Board:       board,
				}
				if board != nil {
					board.BreakerID = slot.BreakerID
					board.PowerFeedID = slot.PowerFeedID
					s.boardsByMAC[board.MAC] = board
					s.boardsBySerial[board.Serial] = board
				}
				module.Slots = append(module.Slots, slot)
			}
			tank.Modules = append(tank.Modules, module)
			s.Modules = append(s.Modules, module)
			s.modulesByID[moduleID] = module
		}
		s.Tanks = append(s.Tanks, tank)
	}
	for _, b := range s.eachBoard() {
		if b.State == miningStateDisconnected {
			sample := s.boardSample(b)
			b.DisconnectedSample = &sample
		}
	}
	return s
}

func newHashboard(cfg ContainerConfig, tank, moduleNum int, moduleID string, slot, globalBoard int, now time.Time) *Hashboard {
	return &Hashboard{
		MAC:             boardMAC(cfg, globalBoard),
		Serial:          fmt.Sprintf("CT1-%s-%04d", cfg.InstanceID, globalBoard),
		HWRevision:      "rev-c",
		FirmwareVersion: cfg.Version,
		FirmwareGitHash: "e87a5197",
		ChipID:          asicModel,
		State:           initialMiningState(cfg, tank, moduleNum, slot),
		TankID:          tank,
		ModuleNum:       moduleNum,
		ModuleID:        moduleID,
		SlotIndex:       slot,
		PowerTargetW:    boardPowerTargetW,
		PowerTargetMinW: boardPowerTargetMinW,
		PowerTargetMaxW: boardPowerTargetMaxW,
		StartTime:       now,
		StateChangedAt:  now,
		PreviousState:   initialMiningState(cfg, tank, moduleNum, slot),
	}
}

// The six-tank demo has three healthy tanks, one with mixed failures, one
// with a single board fault, and one with sleeping/disconnected modules.
// Smaller topologies only include scenarios whose coordinates are present.
func initialMiningState(cfg ContainerConfig, tank, moduleNum, slot int) string {
	if !cfg.DemoModuleStates {
		return miningStateMining
	}
	if tank == demoFaultTank {
		switch moduleNum {
		case demoSleepingModule:
			return miningStateIdle
		case demoOfflineModule:
			return miningStateDisconnected
		case demoAttentionModule:
			return miningStateError
		}
	}
	if tank == demoInactiveTank {
		switch moduleNum {
		case demoInactiveSleepingModule:
			return miningStateIdle
		case demoInactiveOfflineModule:
			return miningStateDisconnected
		}
	}
	if tank == demoLightFaultTank && moduleNum == demoLightFaultModule && slot == 0 {
		return miningStateError
	}
	return miningStateMining
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

func boardMAC(cfg ContainerConfig, position int) string {
	namespace := cfg.IdentityNamespace
	if namespace == "" {
		namespace = cfg.InstanceID
	}
	if namespace == "" {
		return fmt.Sprintf("02:C1:00:%02X:%02X:%02X", (position>>16)&0xff, (position>>8)&0xff, position&0xff)
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s/board/%d", namespace, position)))
	digest[0] = (digest[0] | 2) & 0xfe
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", digest[0], digest[1], digest[2], digest[3], digest[4], digest[5])
}

// setVersion records the firmware version a completed software update installed.
func (s *ContainerState) setVersion(version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Version = version
}
