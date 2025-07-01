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
	DefaultLastShareDelay = 600 // seconds ago
)

// Common structs for API responses
type StatusInfo struct {
	Status string `json:"STATUS"`
	When   int64  `json:"when"`
	Code   int    `json:"code"`
	Msg    string `json:"Msg"`
}

type VersionInfo struct {
	BMMiner     string `json:"BMMiner"`
	API         string `json:"API"`
	Miner       string `json:"Miner"`
	CompileTime string `json:"CompileTime"`
	Type        string `json:"Type"`
}

type PoolStatus struct {
	URL        string  `json:"url"`
	User       string  `json:"user"`
	Status     string  `json:"status"`
	Priority   int     `json:"priority"`
	GetWorks   int     `json:"getworks"`
	Accepted   int     `json:"accepted"`
	Rejected   int     `json:"rejected"`
	Discarded  int     `json:"discarded"`
	LastShare  int     `json:"last_share"`
	Difficulty float64 `json:"difficulty"`
	Diff1Share int     `json:"diff1_share"`
}

type DeviceInfo struct {
	ASC      int     `json:"ASC"`
	Name     string  `json:"name"`
	ID       int     `json:"id"`
	Enabled  string  `json:"enabled"`
	Status   string  `json:"status"`
	Temp     float64 `json:"temp"`
	MHS5s    float64 `json:"mhs_5s"`
	MHS30m   float64 `json:"mhs_30m"`
	MHSav    float64 `json:"mhs_av"`
	Accepted int     `json:"accepted"`
	Rejected int     `json:"rejected"`
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
	Username        string
	Password        string
	IsBlinking      bool
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

// StatsResponse for 'stats' command
type StatsResponse struct {
	RPCResponse
	Stats []struct {
		BMMiner     string  `json:"BMMiner"`
		Miner       string  `json:"Miner"`
		CompileTime string  `json:"CompileTime"`
		Type        string  `json:"Type"`
		Stats       float64 `json:"stats"`
		ID          string  `json:"ID"`
		Elapsed     int     `json:"Elapsed"`
		Calls       int     `json:"Calls"`
		Wait        float64 `json:"Wait"`
		Max         float64 `json:"Max"`
		Min         float64 `json:"Min"`
	} `json:"stats"`
}

// SummaryResponse for 'summary' command
type SummaryResponse struct {
	RPCResponse
	Summary []struct {
		Elapsed   int     `json:"elapsed"`
		Rate5s    float64 `json:"rate_5s"`
		Rate30m   float64 `json:"rate_30m"`
		RateAvg   float64 `json:"rate_avg"`
		RateIdeal float64 `json:"rate_ideal"`
		RateUnit  string  `json:"rate_unit"`
		HwAll     int     `json:"hw_all"`
		BestShare int64   `json:"bestshare"`
	} `json:"SUMMARY"`
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
}
