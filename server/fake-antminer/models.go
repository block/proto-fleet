package main

import "sync"

// Constants for default values
const (
	DefaultElapsedTime    = 3600
	DefaultBestShare      = 12345678
	DefaultHashRateUnit   = "TH/s"
	DefaultGetWorks       = 1234
	DefaultAccepted       = 5678
	DefaultRejected       = 0
	DefaultDiscarded      = 0
	DefaultDifficulty     = 1024.0
	DefaultLastShareDelay = 600  // seconds ago
	DefaultTemperature    = 72.0 // Celsius - realistic ASIC chip temperature
	WorkModeNormal        = "0"
	WorkModeSleep         = "1"
)

// Common structs for API responses
type StatusInfo struct {
	Status      string `json:"STATUS"`
	When        int64  `json:"When"`
	Code        int    `json:"Code"`
	Msg         string `json:"Msg"`
	Description string `json:"Description,omitempty"`
}

type VersionInfo struct {
	BMMiner     string `json:"BMMiner"`
	API         string `json:"API"`
	Miner       string `json:"Miner"`
	CompileTime string `json:"CompileTime"`
	Type        string `json:"Type"`
}

type PoolStatus struct {
	URL            string  `json:"url"`
	User           string  `json:"user"`
	Status         string  `json:"status"`
	Priority       int     `json:"priority"`
	GetWorks       int     `json:"getworks"`
	Accepted       int     `json:"accepted"`
	Rejected       int     `json:"rejected"`
	Discarded      int     `json:"discarded"`
	LastShare      int     `json:"last_share"`
	Difficulty     float64 `json:"difficulty"`
	Diff1Share     int     `json:"diff1_share"`
	GetFailures    int64   `json:"Get Failures"`
	RemoteFailures int64   `json:"Remote Failures"`
}

type DeviceInfo struct {
	ASC                 int     `json:"ASC"`
	Name                string  `json:"Name"`
	ID                  int     `json:"ID"`
	Enabled             string  `json:"Enabled"`
	Status              string  `json:"Status"`
	Tenperature         float64 `json:"Tenperature"`
	MHSav               float64 `json:"MHS av"`
	MHS5s               float64 `json:"MHS 5s"`
	Accepted            int     `json:"Accepted"`
	Rejected            int     `json:"Rejected"`
	HardwareErrors      int     `json:"Hardware Errors"`
	Utility             float64 `json:"Utility"`
	LastSharePool       int     `json:"Last Share Pool"`
	LastShareTime       int64   `json:"Last Share Time"`
	TotalMH             float64 `json:"Total MH"`
	Diff1Work           int     `json:"Diff1 Work"`
	DifficultyAccepted  int64   `json:"Difficulty Accepted"`
	DifficultyRejected  int     `json:"Difficulty Rejected"`
	LastShareDifficulty int64   `json:"Last Share Difficulty"`
	LastValidWork       int64   `json:"Last Valid Work"`
	DeviceHardwarePerc  float64 `json:"Device Hardware%"`
	DeviceRejectedPerc  float64 `json:"Device Rejected%"`
	DeviceElapsed       int     `json:"Device Elapsed"`
}

// MinerState holds the state data of the fake miner
type MinerState struct {
	mu              sync.RWMutex
	MinerType       string
	SerialNumber    string
	MacAddress      string
	FirmwareVersion string
	IPAddress       string
	Hostname        string
	NetMask         string
	Gateway         string
	DNSServers      string
	HashRate        float64
	Temperature     float64
	Pools           []Pool
	MinerMode       string
	BitmainWorkMode string
	Username        string
	Password        string
	IsBlinking      bool
	// Error simulation fields
	ErrorConfig ErrorConfig
}

// ErrorConfig holds configuration for simulating various error conditions
type ErrorConfig struct {
	// Temperature errors
	BoardTemperature float64 // Temperature for board (0 = use default)

	// Hardware error rates
	HWErrorPercent float64 // Hardware error percentage (e.g., 5.0 for 5%)
	HWErrorCount   int     // Absolute hardware error count

	// Share rejection
	RejectedPercent float64 // Rejected share percentage
	RejectedCount   int     // Absolute rejected count
	StaleCount      int     // Stale share count

	// Hashboard status
	BoardDisabled   bool // Set board to disabled
	BoardNotAlive   bool // Set board status to not "Alive"
	BoardNotHashing bool // Set board hashrate to 0
	FanFailed       bool // Set a cooling fan RPM to 0
	PSUFault        bool // Set PSU status to a faulted state

	// Pool connectivity
	PoolNotAlive       bool // Set pool status to not "Alive"
	PoolGetFailures    int  // Pool get failure count
	PoolRemoteFailures int  // Pool remote failure count
}

// Pool represents a mining pool configuration
type Pool struct {
	URL  string
	User string
	Pass string
}

// RPCRequest represents a request to the cgminer API
type RPCRequest struct {
	Command string `json:"command"`
}

// RPCResponse base structure for RPC responses
type RPCResponse struct {
	Status []StatusInfo `json:"status"`
}

// VersionResponse for 'version' command
type VersionResponse struct {
	RPCResponse
	Version []VersionInfo `json:"version"`
}

// SummaryResponse for 'summary' command
type SummaryResponse struct {
	RPCResponse
	Summary []SummaryInfo `json:"SUMMARY"`
	ID      int           `json:"id"`
}

type SummaryInfo struct {
	Elapsed            int     `json:"Elapsed"`
	GHS5s              float64 `json:"GHS 5s"`
	GHSav              float64 `json:"GHS av"`
	GHS30m             float64 `json:"GHS 30m"`
	FoundBlocks        int     `json:"Found Blocks"`
	Getwork            int     `json:"Getwork"`
	Accepted           int     `json:"Accepted"`
	Rejected           int     `json:"Rejected"`
	HardwareErrors     int     `json:"Hardware Errors"`
	Utility            float64 `json:"Utility"`
	Discarded          int     `json:"Discarded"`
	Stale              int     `json:"Stale"`
	GetFailures        int     `json:"Get Failures"`
	LocalWork          int     `json:"Local Work"`
	RemoteFailures     int     `json:"Remote Failures"`
	NetworkBlocks      int     `json:"Network Blocks"`
	TotalMH            float64 `json:"Total MH"`
	WorkUtility        float64 `json:"Work Utility"`
	DifficultyAccepted float64 `json:"Difficulty Accepted"`
	DifficultyRejected float64 `json:"Difficulty Rejected"`
	DifficultyStale    float64 `json:"Difficulty Stale"`
	BestShare          int64   `json:"Best Share"`
	DeviceHardwarePerc float64 `json:"Device Hardware%"`
	DeviceRejectedPerc float64 `json:"Device Rejected%"`
	PoolRejectedPerc   float64 `json:"Pool Rejected%"`
	PoolStalePerc      float64 `json:"Pool Stale%"`
	LastGetwork        int64   `json:"Last getwork"`
}

// PoolsResponse for 'pools' command
type PoolsResponse struct {
	RPCResponse
	Pools []PoolStatus `json:"POOLS"`
}

// DevsResponse for 'devs' command
type DevsResponse struct {
	RPCResponse
	Devices []DeviceInfo `json:"DEVS"`
	ID      int          `json:"id"`
}

func (s *MinerState) currentWorkModeLocked() string {
	switch {
	case s.MinerMode != "":
		return s.MinerMode
	case s.BitmainWorkMode != "":
		return s.BitmainWorkMode
	default:
		return WorkModeNormal
	}
}

func (s *MinerState) setWorkModeLocked(minerMode, bitmainWorkMode string) {
	switch {
	case minerMode != "":
		s.MinerMode = minerMode
		s.BitmainWorkMode = ""
	case bitmainWorkMode != "":
		s.MinerMode = ""
		s.BitmainWorkMode = bitmainWorkMode
	}
}

func (s *MinerState) effectiveHashRateLocked() float64 {
	if s.currentWorkModeLocked() == WorkModeSleep {
		return 0
	}

	return s.HashRate
}

func (s *MinerState) summaryStatusLocked() string {
	if s.currentWorkModeLocked() == WorkModeSleep {
		return "sleeping"
	}

	return "running"
}
