package ct1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Client serializes token refresh with collection. Credentials never enter URLs
// or error messages, and redirects cannot forward them to a different endpoint.
type Client struct {
	baseURL     string
	http        *http.Client
	credentials sdk.UsernamePassword
	token       string
	mu          sync.Mutex
}

func NewClient(info sdk.DeviceInfo, credentials sdk.UsernamePassword) (*Client, error) {
	if info.Host == "" || info.Port <= 0 || info.Port > 65535 || (info.URLScheme != "http" && info.URLScheme != "https") {
		return nil, fmt.Errorf("invalid CT1 endpoint")
	}
	u := url.URL{Scheme: info.URLScheme, Host: net.JoinHostPort(info.Host, strconv.Itoa(int(info.Port)))}
	return &Client{baseURL: u.String(), credentials: credentials, http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) request(ctx context.Context, method, path string, body any, out any) (int, error) {
	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("prepare CT1 request: %w", err)
		}
		payload = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, payload)
	if err != nil {
		return 0, fmt.Errorf("prepare CT1 request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("CT1 %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("CT1 %s returned HTTP %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("decode CT1 %s: %w", path, err)
	}
	return resp.StatusCode, nil
}
func (c *Client) login(ctx context.Context) error {
	var result struct {
		Token string `json:"token"`
	}
	c.token = ""
	_, err := c.request(ctx, http.MethodPost, "/api/v2/auth/login", map[string]string{"username": c.credentials.Username, "password": c.credentials.Password}, &result)
	if err != nil {
		return sdk.NewErrorAuthenticationFailed("CT1", err)
	}
	if result.Token == "" {
		return sdk.NewErrorAuthenticationFailed("CT1", fmt.Errorf("login returned no token"))
	}
	c.token = result.Token
	return nil
}
func (c *Client) get(ctx context.Context, path string, out any) error {
	status, err := c.request(ctx, http.MethodGet, path, nil, out)
	if status == http.StatusUnauthorized && c.credentials.Password != "" {
		if err = c.login(ctx); err != nil {
			return err
		}
		status, err = c.request(ctx, http.MethodGet, path, nil, out)
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return sdk.NewErrorAuthenticationFailed("CT1", err)
	}
	return err
}
func (c *Client) Discover(ctx context.Context, info sdk.DeviceInfo) (sdk.DeviceInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var result struct {
		Info *struct {
			ID      string `json:"id"`
			Product string `json:"product"`
			Model   string `json:"model"`
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := c.get(ctx, "/api/v2/system/info", &result); err != nil {
		return sdk.DeviceInfo{}, err
	}
	if result.Info == nil || strings.TrimSpace(result.Info.ID) == "" || (!strings.EqualFold(result.Info.Product, "Frigate") && !strings.EqualFold(result.Info.Product, "CT1") && !strings.EqualFold(result.Info.Model, "CT1")) {
		return sdk.DeviceInfo{}, fmt.Errorf("endpoint is not a CT1 container")
	}
	info.DeviceClass = sdk.DeviceClassContainer
	info.SerialNumber = result.Info.ID
	info.Model = "CT1"
	info.Manufacturer = "Proto"
	info.FirmwareVersion = result.Info.Version
	return info, nil
}
func (c *Client) Collect(ctx context.Context, id string) (sdk.DeviceMetrics, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var wire MapResponse
	if err := c.get(ctx, "/api/v2/container/map", &wire); err != nil {
		return sdk.DeviceMetrics{}, err
	}
	observed := time.Now().UTC()
	topology, err := snapshot(wire, observed)
	if err != nil {
		return sdk.DeviceMetrics{}, err
	}
	var mining struct {
		Hashrate *float64 `json:"hashrate"`
		Power    *float64 `json:"power"`
		Mining   bool     `json:"mining"`
	}
	result := sdk.DeviceMetrics{DeviceID: id, Timestamp: observed, Container: topology, Health: sdk.HealthWarning}
	if err := c.collectSupplement(ctx, "/api/v2/mining/status", &mining); err != nil {
		slog.WarnContext(ctx, "CT1 mining totals unavailable", "device_id", id, "error", err)
	} else {
		result.Health = sdk.HealthHealthyInactive
		if mining.Mining {
			result.Health = sdk.HealthHealthyActive
		}
		// MDK v2 hashrate is TH/s; the SDK and Timescale use H/s.
		result.HashrateHS = metric(mining.Hashrate, 1e12)
		result.PowerW = metric(mining.Power, 1)
	}
	for _, tank := range topology.Tanks {
		for _, m := range tank.Modules {
			if m.State != sdk.ModuleHashing && m.State != sdk.ModuleSleeping && m.State != sdk.ModuleIsolated {
				result.Health = sdk.HealthWarning
			}
		}
	}
	var plant plantResponse
	if err := c.collectSupplement(ctx, "/api/v2/telemetry/container", &plant); err != nil {
		slog.WarnContext(ctx, "CT1 plant observation unavailable", "device_id", id, "error", err)
	} else if topology.Plant = plant.observation(time.Now().UTC()); topology.Plant != nil {
		// Prefer metered facility power over the mining-only estimate when present.
		p := topology.Plant
		if p.FacilityPowerW != nil {
			result.PowerW = metric(p.FacilityPowerW, 1)
		} else if p.TotalPowerW != nil {
			result.PowerW = metric(p.TotalPowerW, 1)
		}
		result.TempC = metric(p.InletTemperatureC, 1)
		if p.AlarmActive || p.CriticalAlarmActive {
			result.Health = sdk.HealthWarning
		}
	}
	// CT1 does not report efficiency. Use the final controller power selection
	// (including facility loads when available), not summed module PSU power.
	// W / H/s is J/H; the existing telemetry pipeline displays this as J/TH.
	if result.HashrateHS != nil && result.PowerW != nil &&
		result.HashrateHS.Value > 0 && !math.IsInf(result.HashrateHS.Value, 0) && result.PowerW.Value >= 0 {
		efficiency := result.PowerW.Value / result.HashrateHS.Value
		result.EfficiencyJH = metric(&efficiency, 1)
	}
	if err := ctx.Err(); err != nil {
		return sdk.DeviceMetrics{}, fmt.Errorf("collect CT1: %w", err)
	}
	return result, nil
}

// Supplementary totals are optional. Bound request/login/retry and leave
// time for the core result to cross the plugin RPC before the caller expires.
func (c *Client) collectSupplement(ctx context.Context, path string, out any) error {
	budget := time.Second
	if deadline, ok := ctx.Deadline(); ok {
		budget = min(budget, time.Until(deadline)-500*time.Millisecond)
	}
	if budget <= 0 {
		return fmt.Errorf("insufficient collection time for supplementary observation")
	}
	plantCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	return c.get(plantCtx, path, out)
}

func metric(value *float64, multiplier float64) *sdk.MetricValue {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return nil
	}
	return &sdk.MetricValue{Value: *value * multiplier, Kind: sdk.MetricKindGauge}
}
