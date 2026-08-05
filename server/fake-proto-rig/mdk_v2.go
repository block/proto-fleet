// Package main — MDK v2 simulation (PROTOTYPE, throwaway).
//
// The single-miner-view prototype needs a miner that speaks a *different* API
// than today's MDK v1 REST surface, to exercise version-routing (Strategy 2)
// and backend adapters (Strategy 3). Real MDK v2 lives in the miner-firmware
// repo; here we fake a plausibly-divergent shape:
//
//   - a single consolidated GET /api/v2/miner (vs v1's many endpoints)
//   - a wrapped envelope { apiVersion, data, meta }
//   - camelCase fields, hashrate in GH/s (not TH/s), nested thermals
//   - per-chip "chips" array with a state enum (vs v1 "asics")
//
// Enable with MDK_VERSION=2. A public GET /api/version probe lets a client pick
// the right client/adapter. These endpoints send permissive CORS headers so a
// browser-based adapter can call the miner directly (a real finding for
// Strategy 3: direct browser→miner calls need CORS on the device).
package main

import (
	"net/http"
	"os"
	"strconv"
	"time"
)

const defaultFirmwareRev = "1.4.2"

// getMDKVersion reports the simulated firmware generation ("1" or "2").
func getMDKVersion() string {
	if v := os.Getenv("MDK_VERSION"); v == "2" {
		return "2"
	}
	return "1"
}

func getFirmwareRev() string {
	return getEnv("FIRMWARE_REV", defaultFirmwareRev)
}

func withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-MDK-Key")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

// RegisterV2Routes wires the version probe (always) and the divergent v2
// consolidated endpoint (only when MDK_VERSION=2).
func (h *RESTApiHandler) RegisterV2Routes(mux *http.ServeMux) {
	mdk := getMDKVersion()

	mux.HandleFunc("/api/version", withCORS(func(w http.ResponseWriter, _ *http.Request) {
		apiVersions := []string{"v1"}
		if mdk == "2" {
			apiVersions = []string{"v1", "v2"}
		}
		h.writeJSON(w, http.StatusOK, map[string]any{
			"mdkVersion":  mdk,
			"apiVersions": apiVersions,
			"firmwareRev": getFirmwareRev(),
		})
	}))

	if mdk == "2" {
		mux.HandleFunc("/api/v2/miner", withCORS(h.handleV2Miner))
	}
}

// v2 envelope types — deliberately different from the v1 JSON shapes.
type v2Envelope struct {
	APIVersion string `json:"apiVersion"`
	Data       v2Data `json:"data"`
	Meta       v2Meta `json:"meta"`
}

type v2Meta struct {
	GeneratedAt string `json:"generatedAt"`
	Schema      string `json:"schema"`
}

type v2Data struct {
	Device      v2Device      `json:"device"`
	State       string        `json:"state"`
	Performance v2Performance `json:"performance"`
	Boards      []v2Board     `json:"boards"`
}

type v2Device struct {
	DisplayName   string `json:"displayName"`
	HardwareModel string `json:"hardwareModel"`
	FirmwareRev   string `json:"firmwareRev"`
	MDK           string `json:"mdk"`
	NetMAC        string `json:"netMac"`
	UnitSerial    string `json:"unitSerial"`
	LANIP         string `json:"lanIp"`
}

type v2Performance struct {
	HashrateGHS float64   `json:"hashrateGhs"`
	PowerWatts  float64   `json:"powerWatts"`
	Thermals    v2Thermal `json:"thermals"`
}

type v2Thermal struct {
	PeakC float64 `json:"peakC"`
	AvgC  float64 `json:"avgC"`
}

type v2Board struct {
	Slot        int       `json:"slot"`
	Serial      string    `json:"serial"`
	HashrateGHS float64   `json:"hashrateGhs"`
	Thermals    v2Thermal `json:"thermals"`
	Chips       []v2Chip  `json:"chips"`
}

type v2Chip struct {
	Pos   int     `json:"pos"`
	TempC float64 `json:"tempC"`
	GHS   float64 `json:"ghs"`
	State string  `json:"state"` // ONLINE | HOT | FAULT | OFFLINE
}

const chipsPerBoardV2 = 66

func (h *RESTApiHandler) handleV2Miner(w http.ResponseWriter, _ *http.Request) {
	hashrateTHS, tempC, powerW, _ := h.state.GetMinerTelemetry()
	boardCount := h.state.GetHashboardCount()

	boards := make([]v2Board, 0, boardCount)
	for slot := 0; slot < boardCount; slot++ {
		baseTemp := tempC + float64(slot)*2
		inError := h.state.IsHashboardInError(slot)
		chips := make([]v2Chip, 0, chipsPerBoardV2)
		var boardGHS float64
		var peak float64
		for pos := 0; pos < chipsPerBoardV2; pos++ {
			wobble := float64((pos*7+slot*13)%11) - 5
			ct := baseTemp + wobble
			state := "ONLINE"
			switch {
			case inError && pos%5 == 0:
				state = "FAULT"
			case ct >= baseTemp+4:
				state = "HOT"
			case (pos*3+slot)%37 == 0:
				state = "OFFLINE"
			}
			ghs := 290.0 + wobble*4
			if state == "OFFLINE" || state == "FAULT" {
				ghs = 0
			}
			if ct > peak {
				peak = ct
			}
			boardGHS += ghs
			chips = append(chips, v2Chip{Pos: pos, TempC: round1(ct), GHS: round1(ghs), State: state})
		}
		boards = append(boards, v2Board{
			Slot:        slot,
			Serial:      "HB-" + strconv.Itoa(1000+slot),
			HashrateGHS: round1(boardGHS),
			Thermals:    v2Thermal{PeakC: round1(peak), AvgC: round1(baseTemp)},
			Chips:       chips,
		})
	}

	env := v2Envelope{
		APIVersion: "2.0",
		Data: v2Data{
			Device: v2Device{
				DisplayName:   orDefault(h.state.Hostname, "proto-sim"),
				HardwareModel: orDefault(h.state.Model, "Proto Alpha"),
				FirmwareRev:   getFirmwareRev(),
				MDK:           "2.0",
				NetMAC:        h.state.MacAddress,
				UnitSerial:    h.state.SerialNumber,
				LANIP:         h.state.IPAddress,
			},
			State: v2State(string(h.state.GetMiningState())),
			Performance: v2Performance{
				HashrateGHS: round1(hashrateTHS * 1000),
				PowerWatts:  round1(powerW),
				Thermals:    v2Thermal{PeakC: round1(tempC + 12), AvgC: round1(tempC)},
			},
			Boards: boards,
		},
		Meta: v2Meta{GeneratedAt: time.Now().UTC().Format(time.RFC3339), Schema: "mdk-v2-consolidated"},
	}
	h.writeJSON(w, http.StatusOK, env)
}

func v2State(miningState string) string {
	if miningState == "mining" || miningState == "degraded" {
		return "HASHING"
	}
	return "IDLE"
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func round1(v float64) float64 {
	return float64(int(v*10)) / 10
}
