package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"connectrpc.com/connect"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_data_api"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_system_api"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_system_api/miner_system_apiconnect"
)

var _ miner_system_apiconnect.MinerSystemApiHandler = (*SystemApiHandler)(nil)

// SystemApiHandler implements MinerSystemApi for the fake miner.
type SystemApiHandler struct {
	state *MinerState
}

// NewSystemApiHandler creates a new SystemApiHandler.
func NewSystemApiHandler(state *MinerState) *SystemApiHandler {
	return &SystemApiHandler{state: state}
}

// GetNetwork returns network configuration.
func (h *SystemApiHandler) GetNetwork(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_system_api.GetNetworkResponse], error) {
	h.state.mu.RLock()
	defer h.state.mu.RUnlock()

	return connect.NewResponse(&miner_system_api.GetNetworkResponse{
		Mac:      h.state.MacAddress,
		Dhcp:     h.state.DHCP,
		Ip:       h.state.IPAddress,
		Netmask:  h.state.NetMask,
		Gateway:  h.state.Gateway,
		Hostname: h.state.Hostname,
	}), nil
}

// SetNetwork configures network settings.
func (h *SystemApiHandler) SetNetwork(ctx context.Context, req *connect.Request[miner_system_api.SetNetworkRequest]) (*connect.Response[miner_system_api.SetNetworkResponse], error) {
	h.state.mu.Lock()
	defer h.state.mu.Unlock()

	// Handle DHCP vs static configuration using getter methods
	if req.Msg.GetDhcpConfig() != nil {
		h.state.DHCP = true
		log.Printf("Network set to DHCP (SN: %s)", h.state.SerialNumber)
	} else if staticConfig := req.Msg.GetStaticConfig(); staticConfig != nil {
		h.state.DHCP = false
		h.state.IPAddress = staticConfig.Ip
		h.state.NetMask = staticConfig.Netmask
		h.state.Gateway = staticConfig.Gateway
		log.Printf("Network set to static: IP=%s (SN: %s)", staticConfig.Ip, h.state.SerialNumber)
	}

	// Handle optional hostname
	hostname := req.Msg.GetHostname()
	if hostname != "" {
		h.state.Hostname = hostname
	}

	return connect.NewResponse(&miner_system_api.SetNetworkResponse{}), nil
}

// Reboot reboots the miner.
func (h *SystemApiHandler) Reboot(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_common_api.ApiResultResponse], error) {
	log.Printf("Reboot requested (SN: %s)", h.state.SerialNumber)

	// In a real implementation, this would trigger a system reboot.
	// For simulation, we just log the request and return success.

	return connect.NewResponse(&miner_common_api.ApiResultResponse{
		Result: miner_common_api.ApiResult_RESULT_SUCCESS,
	}), nil
}

// GetLogs returns system logs.
func (h *SystemApiHandler) GetLogs(ctx context.Context, req *connect.Request[miner_system_api.GetLogsRequest]) (*connect.Response[miner_system_api.GetLogsResponse], error) {
	lines := uint32(100)
	if req.Msg.Lines != nil {
		lines = *req.Msg.Lines
	}

	// Generate simulated log entries
	logContent := make([]string, 0, lines)
	now := time.Now()

	for i := uint32(0); i < lines && i < 1000; i++ {
		timestamp := now.Add(-time.Duration(lines-i) * time.Second).Format(time.RFC3339)
		logContent = append(logContent, fmt.Sprintf("[%s] Mining operation normal - hashrate: 140.0 TH/s", timestamp))
	}

	return connect.NewResponse(&miner_system_api.GetLogsResponse{
		Source:  req.Msg.Source,
		Lines:   uint32(len(logContent)),
		Content: logContent,
	}), nil
}

// Update initiates a firmware update.
func (h *SystemApiHandler) Update(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_system_api.UpdateResponse], error) {
	log.Printf("Firmware update requested (SN: %s)", h.state.SerialNumber)

	return connect.NewResponse(&miner_system_api.UpdateResponse{
		Message: "Update initiated. System will reboot when complete.",
	}), nil
}

// Upload handles firmware upload via streaming.
func (h *SystemApiHandler) Upload(ctx context.Context, stream *connect.ClientStream[miner_system_api.UploadRequest]) (*connect.Response[miner_system_api.UploadResponse], error) {
	var filename string
	var totalBytes int64

	for stream.Receive() {
		msg := stream.Msg()
		switch data := msg.Data.(type) {
		case *miner_system_api.UploadRequest_Metadata:
			filename = data.Metadata.FileName
			log.Printf("Upload started: %s (SN: %s)", filename, h.state.SerialNumber)

		case *miner_system_api.UploadRequest_Chunk:
			totalBytes += int64(len(data.Chunk))
		}
	}

	if err := stream.Err(); err != nil && err != io.EOF {
		return nil, err
	}

	log.Printf("Upload complete: %s (%d bytes) (SN: %s)", filename, totalBytes, h.state.SerialNumber)

	return connect.NewResponse(&miner_system_api.UploadResponse{
		Message: fmt.Sprintf("Uploaded %s (%d bytes)", filename, totalBytes),
	}), nil
}

// SchedulePsuFirmwareUpdate schedules a PSU firmware update.
func (h *SystemApiHandler) SchedulePsuFirmwareUpdate(ctx context.Context, req *connect.Request[miner_system_api.SchedulePsuFirmwareUpdateRequest]) (*connect.Response[miner_system_api.SchedulePsuFirmwareUpdateResponse], error) {
	log.Printf("PSU firmware update scheduled (force: %v, SN: %s)", req.Msg.Force, h.state.SerialNumber)

	return connect.NewResponse(&miner_system_api.SchedulePsuFirmwareUpdateResponse{
		Result:  miner_common_api.ApiResult_RESULT_SUCCESS,
		Message: "PSU firmware update scheduled for next reboot",
	}), nil
}

// FactoryReset performs a factory reset.
func (h *SystemApiHandler) FactoryReset(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_common_api.ApiResultResponse], error) {
	log.Printf("Factory reset requested (SN: %s)", h.state.SerialNumber)

	// Clear state as if factory reset
	h.state.mu.Lock()
	h.state.AuthPublicKey = ""
	h.state.Pools = make([]*miner_data_api.Pool, 0)
	h.state.mu.Unlock()

	return connect.NewResponse(&miner_common_api.ApiResultResponse{
		Result: miner_common_api.ApiResult_RESULT_SUCCESS,
	}), nil
}

// ClearUserSettings clears user settings.
func (h *SystemApiHandler) ClearUserSettings(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_common_api.ApiResultResponse], error) {
	log.Printf("Clear user settings requested (SN: %s)", h.state.SerialNumber)

	// Reset to defaults (but keep auth key)
	h.state.mu.Lock()
	h.state.Pools = make([]*miner_data_api.Pool, 0)
	h.state.PowerTargetW = defaultPowerTargetW
	h.state.PerformanceMode = miner_data_api.PerformanceMode_PERFORMANCE_MODE_MAXIMUM_HASHRATE
	h.state.CoolingMode = miner_data_api.CoolingMode_COOLING_MODE_AUTO
	h.state.FanSpeedPct = defaultFanSpeedPct
	h.state.mu.Unlock()

	return connect.NewResponse(&miner_common_api.ApiResultResponse{
		Result: miner_common_api.ApiResult_RESULT_SUCCESS,
	}), nil
}

// PairingApiHandler implements MinerPairingApi for the fake miner.
// Note: This is a separate service from MinerSystemApi with no auth required.
type PairingApiHandler struct {
	state *MinerState
}

// NewPairingApiHandler creates a new PairingApiHandler.
func NewPairingApiHandler(state *MinerState) *PairingApiHandler {
	return &PairingApiHandler{state: state}
}

var _ miner_system_apiconnect.MinerPairingApiHandler = (*PairingApiHandler)(nil)

// SetAuthKey sets the authentication public key for pairing.
func (h *PairingApiHandler) SetAuthKey(ctx context.Context, req *connect.Request[miner_system_api.SetAuthKeyRequest]) (*connect.Response[miner_system_api.SetAuthKeyResponse], error) {
	publicKey := req.Msg.PublicKey

	if publicKey == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("public key cannot be empty"))
	}

	h.state.SetAuthKey(publicKey)
	log.Printf("Auth key set (SN: %s, key: %s...)", h.state.SerialNumber, truncateKey(publicKey))

	return connect.NewResponse(&miner_system_api.SetAuthKeyResponse{
		Message: "Auth key set successfully",
	}), nil
}

// ClearAuthKey clears the authentication public key.
func (h *PairingApiHandler) ClearAuthKey(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_common_api.ApiResultResponse], error) {
	h.state.ClearAuthKey()
	log.Printf("Auth key cleared (SN: %s)", h.state.SerialNumber)

	return connect.NewResponse(&miner_common_api.ApiResultResponse{
		Result: miner_common_api.ApiResult_RESULT_SUCCESS,
	}), nil
}

// GetPairingInfo returns device identification for discovery.
func (h *PairingApiHandler) GetPairingInfo(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_system_api.GetPairingInfoResponse], error) {
	h.state.mu.RLock()
	defer h.state.mu.RUnlock()

	return connect.NewResponse(&miner_system_api.GetPairingInfoResponse{
		Mac:  h.state.MacAddress,
		CbSn: h.state.SerialNumber,
	}), nil
}

// truncateKey returns a truncated version of the key for logging.
func truncateKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:12]
}
