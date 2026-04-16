package rpc

import "encoding/json"

type RPCRequest struct {
	Command   string `json:"command"`
	Parameter string `json:"parameter,omitempty"`
}

type RPCResponse struct {
	Status []StatusInfo `json:"STATUS,omitempty"`
	ID     int          `json:"id,omitempty"`
}

type StatusInfo struct {
	Status      string `json:"STATUS"`
	When        int64  `json:"When"`
	Code        int    `json:"Code"`
	Msg         string `json:"Msg"`
	Description string `json:"Description"`
}

type VersionResponse struct {
	Status  []StatusInfo  `json:"STATUS"`
	Version []VersionInfo `json:"VERSION"`
	ID      int           `json:"id"`
}

type VersionInfo struct {
	BMMiner     string `json:"BMMiner"`
	LUXminer    string `json:"LUXminer"`
	API         string `json:"API"`
	Miner       string `json:"Miner"`
	CompileTime string `json:"CompileTime"`
	Type        string `json:"Type"`
}

type SummaryResponse struct {
	Status  []StatusInfo  `json:"STATUS"`
	Summary []SummaryInfo `json:"SUMMARY"`
	ID      int           `json:"id"`
}

type SummaryInfo struct {
	// All SummaryInfo fields:
	Elapsed               int64   `json:"Elapsed"`
	GHS5s                 float64 `json:"GHS 5s"`
	GHSAv                 float64 `json:"GHS av"`
	GHS30m                float64 `json:"GHS 30m"`
	FoundBlocks           int64   `json:"Found Blocks"`
	Getwork               int64   `json:"Getwork"`
	Accepted              int64   `json:"Accepted"`
	Rejected              int64   `json:"Rejected"`
	HardwareErrors        int64   `json:"Hardware Errors"`
	Utility               float64 `json:"Utility"`
	Discarded             int64   `json:"Discarded"`
	Stale                 int64   `json:"Stale"`
	GetFailures           int64   `json:"Get Failures"`
	LocalWork             int64   `json:"Local Work"`
	RemoteFailures        int64   `json:"Remote Failures"`
	NetworkBlocks         int64   `json:"Network Blocks"`
	TotalMH               float64 `json:"Total MH"`
	WorkUtility           float64 `json:"Work Utility"`
	DifficultyAccepted    float64 `json:"Difficulty Accepted"`
	DifficultyRejected    float64 `json:"Difficulty Rejected"`
	DifficultyStale       float64 `json:"Difficulty Stale"`
	BestShare             int64   `json:"Best Share"`
	DeviceHardwarePercent float64 `json:"Device Hardware%"`
	DeviceRejectedPercent float64 `json:"Device Rejected%"`
	PoolRejectedPercent   float64 `json:"Pool Rejected%"`
	PoolStalePercent      float64 `json:"Pool Stale%"`
	LastGetwork           int64   `json:"Last getwork"`
}

type PoolsResponse struct {
	Status []StatusInfo `json:"STATUS"`
	Pools  []PoolInfo   `json:"POOLS"`
	ID     int          `json:"id"`
}

type PoolInfo struct {
	Pool                int     `json:"POOL"`
	URL                 string  `json:"URL"`
	Status              string  `json:"Status"`
	Priority            int     `json:"Priority"`
	Quota               int     `json:"Quota"`
	LongPoll            string  `json:"Long Poll"`
	Getworks            int64   `json:"Getworks"`
	Accepted            int64   `json:"Accepted"`
	Rejected            int64   `json:"Rejected"`
	Discarded           int64   `json:"Discarded"`
	Stale               int64   `json:"Stale"`
	GetFailures         int64   `json:"Get Failures"`
	RemoteFailures      int64   `json:"Remote Failures"`
	User                string  `json:"User"`
	LastShareTime       string  `json:"Last Share Time"`
	Diff                string  `json:"Diff"`
	Diff1Shares         int64   `json:"Diff1 Shares"`
	ProxyType           string  `json:"Proxy Type"`
	Proxy               string  `json:"Proxy"`
	DifficultyAccepted  float64 `json:"Difficulty Accepted"`
	DifficultyRejected  float64 `json:"Difficulty Rejected"`
	DifficultyStale     float64 `json:"Difficulty Stale"`
	LastShareDifficulty float64 `json:"Last Share Difficulty"`
	HasStratum          bool    `json:"Has Stratum"`
	StratumActive       bool    `json:"Stratum Active"`
	StratumURL          string  `json:"Stratum URL"`
	HasGBT              bool    `json:"Has GBT"`
	BestShare           float64 `json:"Best Share"`
	PoolRejectedPercent float64 `json:"Pool Rejected%"`
	PoolStalePercent    float64 `json:"Pool Stale%"`
}

type DevsResponse struct {
	Status []StatusInfo `json:"STATUS"`
	Devs   []DevInfo    `json:"DEVS"`
	ID     int          `json:"id"`
}

type DevInfo struct {
	ASC                   int     `json:"ASC"`
	Name                  string  `json:"Name"`
	ID                    int     `json:"ID"`
	Enabled               string  `json:"Enabled"`
	Status                string  `json:"Status"`
	Temperature           float64 `json:"Temperature"` // Correct spelling
	Tenperature           float64 `json:"Tenperature"` // TYPO REQUIRED: Antminer firmware actually uses this misspelling
	MHSAv                 float64 `json:"MHS av"`
	MHS5s                 float64 `json:"MHS 5s"`
	Accepted              int64   `json:"Accepted"`
	Rejected              int64   `json:"Rejected"`
	HardwareErrors        int64   `json:"Hardware Errors"`
	Utility               float64 `json:"Utility"`
	LastSharePool         int     `json:"Last Share Pool"`
	LastShareTime         int64   `json:"Last Share Time"`
	TotalMH               float64 `json:"Total MH"`
	Diff1Work             int64   `json:"Diff1 Work"`
	DifficultyAccepted    float64 `json:"Difficulty Accepted"`
	DifficultyRejected    float64 `json:"Difficulty Rejected"`
	LastShareDifficulty   float64 `json:"Last Share Difficulty"`
	LastValidWork         int64   `json:"Last Valid Work"`
	DeviceHardwarePercent float64 `json:"Device Hardware%"`
	DeviceRejectedPercent float64 `json:"Device Rejected%"`
	DeviceElapsed         int64   `json:"Device Elapsed"`
}

// GetTemperature returns the temperature value of the device.
// Checks both correct spelling (Temperature) and the typo (Tenperature) used by Antminer firmware.
func (d *DevInfo) GetTemperature() float64 {
	if d.Temperature != 0 {
		return d.Temperature
	}
	return d.Tenperature
}

type ConfigResponse struct {
	Status []StatusInfo `json:"STATUS"`
	Config []ConfigInfo `json:"CONFIG"`
	ID     int          `json:"id"`
}

type ConfigInfo struct {
	ASCCount    int    `json:"ASC Count"`
	PGACount    int    `json:"PGA Count"`
	PoolCount   int    `json:"Pool Count"`
	Strategy    string `json:"Strategy"`
	LogInterval int    `json:"Log Interval"`
	DeviceCode  string `json:"Device Code"`
	OS          string `json:"OS"`
}

// StatsResponse represents the response from the "stats" RPC command.
// The STATS array contains mixed types: the first element is firmware metadata,
// and the second element contains the actual mining stats (including power).
type StatsResponse struct {
	Status []StatusInfo      `json:"STATUS"`
	Stats  []json.RawMessage `json:"STATS"`
	ID     int               `json:"id"`
}
