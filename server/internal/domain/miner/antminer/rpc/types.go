package rpc

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
	API         string `json:"API"`
	Miner       string `json:"Miner"`
	CompileTime string `json:"CompileTime"`
	Type        string `json:"Type"`
}

type StatsResponse struct {
	Status []StatusInfo `json:"STATUS"`
	Stats  []StatsInfo  `json:"STATS"`
	ID     int          `json:"id"`
}

type StatsInfo struct {
	// All StatsInfo fields:
	BMMiner        string  `json:"BMMiner,omitempty"`
	Miner          string  `json:"Miner,omitempty"`
	CompileTime    string  `json:"CompileTime,omitempty"`
	Type           string  `json:"Type,omitempty"`
	StatsID        int     `json:"STATS,omitempty"`
	ID             string  `json:"ID,omitempty"`
	Elapsed        int64   `json:"Elapsed,omitempty"`
	Calls          int     `json:"Calls,omitempty"`
	Wait           int     `json:"Wait,omitempty"`
	Max            int     `json:"Max,omitempty"`
	Min            int     `json:"Min,omitempty"`
	GHS5s          float64 `json:"GHS 5s,omitempty"`
	GHSAv          float64 `json:"GHS av,omitempty"`
	Rate30m        float64 `json:"rate_30m,omitempty"`
	Mode           int     `json:"Mode,omitempty"`
	MinerCount     int     `json:"miner_count,omitempty"`
	Frequency      int     `json:"frequency,omitempty"`
	FanNum         int     `json:"fan_num,omitempty"`
	Fan1           int     `json:"fan1,omitempty"`
	Fan2           int     `json:"fan2,omitempty"`
	Fan3           int     `json:"fan3,omitempty"`
	Fan4           int     `json:"fan4,omitempty"`
	TempNum        int     `json:"temp_num,omitempty"`
	Temp1          int     `json:"temp1,omitempty"`
	Temp2_1        int     `json:"temp2_1,omitempty"`
	Temp2          int     `json:"temp2,omitempty"`
	Temp2_2        int     `json:"temp2_2,omitempty"`
	Temp3          int     `json:"temp3,omitempty"`
	Temp2_3        int     `json:"temp2_3,omitempty"`
	TempPCB1       string  `json:"temp_pcb1,omitempty"`
	TempPCB2       string  `json:"temp_pcb2,omitempty"`
	TempPCB3       string  `json:"temp_pcb3,omitempty"`
	TempPCB4       string  `json:"temp_pcb4,omitempty"`
	TempChip1      string  `json:"temp_chip1,omitempty"`
	TempChip2      string  `json:"temp_chip2,omitempty"`
	TempChip3      string  `json:"temp_chip3,omitempty"`
	TempChip4      string  `json:"temp_chip4,omitempty"`
	TempPIC1       string  `json:"temp_pic1,omitempty"`
	TempPIC2       string  `json:"temp_pic2,omitempty"`
	TempPIC3       string  `json:"temp_pic3,omitempty"`
	TempPIC4       string  `json:"temp_pic4,omitempty"`
	TotalRateIdeal float64 `json:"total_rateideal,omitempty"`
	RateUnit       string  `json:"rate_unit,omitempty"`
	TotalFreqAvg   int     `json:"total_freqavg,omitempty"`
	TotalACN       int     `json:"total_acn,omitempty"`
	TotalRate      float64 `json:"total rate,omitempty"`
	TempMax        int     `json:"temp_max,omitempty"`
	NoMatchingWork int     `json:"no_matching_work,omitempty"`
	ChainACN1      int     `json:"chain_acn1,omitempty"`
	ChainACN2      int     `json:"chain_acn2,omitempty"`
	ChainACN3      int     `json:"chain_acn3,omitempty"`
	ChainACN4      int     `json:"chain_acn4,omitempty"`
	ChainACS1      string  `json:"chain_acs1,omitempty"`
	ChainACS2      string  `json:"chain_acs2,omitempty"`
	ChainACS3      string  `json:"chain_acs3,omitempty"`
	ChainACS4      string  `json:"chain_acs4,omitempty"`
	ChainHW1       int     `json:"chain_hw1,omitempty"`
	ChainHW2       int     `json:"chain_hw2,omitempty"`
	ChainHW3       int     `json:"chain_hw3,omitempty"`
	ChainHW4       int     `json:"chain_hw4,omitempty"`
	ChainRate1     string  `json:"chain_rate1,omitempty"`
	ChainRate2     string  `json:"chain_rate2,omitempty"`
	ChainRate3     string  `json:"chain_rate3,omitempty"`
	ChainRate4     string  `json:"chain_rate4,omitempty"`
	Freq1          int     `json:"freq1,omitempty"`
	Freq2          int     `json:"freq2,omitempty"`
	Freq3          int     `json:"freq3,omitempty"`
	Freq4          int     `json:"freq4,omitempty"`
	MinerVersion   string  `json:"miner_version,omitempty"`
	MinerID        string  `json:"miner_id,omitempty"`
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
	Temperature           float64 `json:"Tenperature"` // Note: Typo is preserved for compatibility
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
