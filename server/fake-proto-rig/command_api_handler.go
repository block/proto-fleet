package main

import (
	"context"
	"fmt"
	"log"

	"connectrpc.com/connect"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_command_api"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_command_api/miner_command_apiconnect"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_data_api"
)

var _ miner_command_apiconnect.MinerCommandApiHandler = (*CommandApiHandler)(nil)

// CommandApiHandler implements MinerCommandApi for the fake miner.
type CommandApiHandler struct {
	state *MinerState
}

// NewCommandApiHandler creates a new CommandApiHandler.
func NewCommandApiHandler(state *MinerState) *CommandApiHandler {
	return &CommandApiHandler{state: state}
}

// StartMining starts mining operations.
func (h *CommandApiHandler) StartMining(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_command_api.CommandResponse], error) {
	currentState := h.state.GetMiningState()

	// Check if already mining
	if currentState == miner_data_api.MiningState_MINING_STATE_MINING {
		return connect.NewResponse(&miner_command_api.CommandResponse{
			Result:  miner_common_api.ApiResult_RESULT_ERR_ALREADY_MINING,
			Message: "already mining",
		}), nil
	}

	// Start mining (pools can be configured later)
	h.state.SetMiningState(miner_data_api.MiningState_MINING_STATE_MINING)
	log.Printf("Mining started (SN: %s)", h.state.SerialNumber)

	return connect.NewResponse(&miner_command_api.CommandResponse{
		Result:  miner_common_api.ApiResult_RESULT_SUCCESS,
		Message: "mining started",
	}), nil
}

// StopMining stops mining operations.
func (h *CommandApiHandler) StopMining(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_command_api.CommandResponse], error) {
	currentState := h.state.GetMiningState()

	// Check if already stopped
	if currentState == miner_data_api.MiningState_MINING_STATE_STOPPED ||
		currentState == miner_data_api.MiningState_MINING_STATE_UNINITIALIZED {
		return connect.NewResponse(&miner_command_api.CommandResponse{
			Result:  miner_common_api.ApiResult_RESULT_ERR_ALREADY_OFF,
			Message: "mining already stopped",
		}), nil
	}

	// Stop mining
	h.state.SetMiningState(miner_data_api.MiningState_MINING_STATE_STOPPED)
	log.Printf("Mining stopped (SN: %s)", h.state.SerialNumber)

	return connect.NewResponse(&miner_command_api.CommandResponse{
		Result:  miner_common_api.ApiResult_RESULT_SUCCESS,
		Message: "mining stopped",
	}), nil
}

// SetPowerTarget configures the power target.
func (h *CommandApiHandler) SetPowerTarget(ctx context.Context, req *connect.Request[miner_command_api.PowerTargetRequest]) (*connect.Response[miner_data_api.PowerTargetResponse], error) {
	powerTarget := req.Msg.PowerTargetW
	perfMode := req.Msg.PerformanceMode

	// Validate power target range
	if powerTarget < defaultPowerTargetMin || powerTarget > defaultPowerTargetMax {
		return connect.NewResponse(&miner_data_api.PowerTargetResponse{
			Result: miner_common_api.ApiResult_RESULT_ERR_OUT_OF_RANGE,
		}), nil
	}

	h.state.SetPowerTarget(powerTarget, perfMode, req.Msg.HashOnDisconnect)
	log.Printf("Power target set: %dW, mode: %v (SN: %s)", powerTarget, perfMode, h.state.SerialNumber)

	h.state.mu.RLock()
	hashOnDisconnect := h.state.HashOnDisconnect
	h.state.mu.RUnlock()

	return connect.NewResponse(&miner_data_api.PowerTargetResponse{
		Result:                miner_common_api.ApiResult_RESULT_SUCCESS,
		PowerTargetW:          powerTarget,
		PerformanceMode:       perfMode,
		PowerTargetMinW:       defaultPowerTargetMin,
		PowerTargetMaxW:       defaultPowerTargetMax,
		DefaultPowerTargetW:   defaultPowerTargetW,
		PhaseBalancingEnabled: req.Msg.PhaseBalancingEnabled,
		HashOnDisconnect:      hashOnDisconnect,
	}), nil
}

// SetCoolingMode configures the cooling system.
func (h *CommandApiHandler) SetCoolingMode(ctx context.Context, req *connect.Request[miner_command_api.CoolingModeRequest]) (*connect.Response[miner_data_api.CoolingModeResponse], error) {
	mode := req.Msg.Mode
	speedPct := req.Msg.SpeedPercentage

	// For manual mode without speed, use default (proto definition marks speed_percentage as optional)
	if mode == miner_data_api.CoolingMode_COOLING_MODE_MANUAL && speedPct == nil {
		defaultSpeed := uint32(defaultFanSpeedPct)
		speedPct = &defaultSpeed
	}

	h.state.SetCoolingMode(mode, speedPct)
	log.Printf("Cooling mode set: %v (SN: %s)", mode, h.state.SerialNumber)

	h.state.mu.RLock()
	currentSpeed := h.state.FanSpeedPct
	h.state.mu.RUnlock()

	return connect.NewResponse(&miner_data_api.CoolingModeResponse{
		Result:          miner_common_api.ApiResult_RESULT_SUCCESS,
		Mode:            mode,
		SpeedPercentage: currentSpeed,
	}), nil
}

// AddPools adds mining pools to the configuration.
func (h *CommandApiHandler) AddPools(ctx context.Context, req *connect.Request[miner_command_api.PoolsRequest]) (*connect.Response[miner_command_api.CommandResponse], error) {
	pools := req.Msg.Pools

	// Get existing pools to check for duplicates
	existingPools := h.state.GetPools()
	existingURLs := make(map[string]bool)
	existingPriorities := make(map[uint32]bool)
	maxIdx := uint32(0)

	for _, pool := range existingPools {
		existingURLs[pool.Url] = true
		existingPriorities[pool.Priority] = true
		if pool.Idx > maxIdx {
			maxIdx = pool.Idx
		}
	}

	// Validate and add pools
	for _, pool := range pools {
		// Check for duplicate URL
		if existingURLs[pool.Url] {
			return connect.NewResponse(&miner_command_api.CommandResponse{
				Result:  miner_common_api.ApiResult_RESULT_ERR_POOL_DUPLICATE_URL,
				Message: fmt.Sprintf("duplicate pool URL: %s", pool.Url),
			}), nil
		}

		// Check for duplicate priority
		if existingPriorities[pool.Priority] {
			return connect.NewResponse(&miner_command_api.CommandResponse{
				Result:  miner_common_api.ApiResult_RESULT_ERR_POOL_DUPLICATE_PRIORITY,
				Message: fmt.Sprintf("duplicate pool priority: %d", pool.Priority),
			}), nil
		}

		// Validate URL is not empty
		if pool.Url == "" {
			return connect.NewResponse(&miner_command_api.CommandResponse{
				Result:  miner_common_api.ApiResult_RESULT_ERR_POOL_INVALID_URL,
				Message: "pool URL cannot be empty",
			}), nil
		}

		// Assign index and add pool
		maxIdx++
		pool.Idx = maxIdx
		pool.ComponentId = maxIdx

		h.state.AddPool(pool)
		existingURLs[pool.Url] = true
		existingPriorities[pool.Priority] = true

		log.Printf("Pool added: %s (priority: %d, SN: %s)", pool.Url, pool.Priority, h.state.SerialNumber)
	}

	return connect.NewResponse(&miner_command_api.CommandResponse{
		Result:  miner_common_api.ApiResult_RESULT_SUCCESS,
		Message: fmt.Sprintf("%d pools added", len(pools)),
	}), nil
}

// RemovePools removes mining pools from the configuration.
func (h *CommandApiHandler) RemovePools(ctx context.Context, req *connect.Request[miner_command_api.PoolsRequest]) (*connect.Response[miner_command_api.CommandResponse], error) {
	poolsToRemove := req.Msg.Pools

	// Collect indices to remove
	indices := make([]uint32, 0, len(poolsToRemove))
	for _, pool := range poolsToRemove {
		indices = append(indices, pool.Idx)
	}

	h.state.RemovePools(indices)
	log.Printf("Pools removed: %v (SN: %s)", indices, h.state.SerialNumber)

	return connect.NewResponse(&miner_command_api.CommandResponse{
		Result:  miner_common_api.ApiResult_RESULT_SUCCESS,
		Message: fmt.Sprintf("%d pools removed", len(poolsToRemove)),
	}), nil
}

// EditPool modifies an existing pool configuration.
func (h *CommandApiHandler) EditPool(ctx context.Context, req *connect.Request[miner_data_api.Pool]) (*connect.Response[miner_command_api.CommandResponse], error) {
	poolToEdit := req.Msg

	h.state.mu.Lock()
	defer h.state.mu.Unlock()

	// Find and update the pool
	found := false
	for i, pool := range h.state.Pools {
		if pool.Idx == poolToEdit.Idx {
			h.state.Pools[i] = poolToEdit
			found = true
			break
		}
	}

	if !found {
		return connect.NewResponse(&miner_command_api.CommandResponse{
			Result:  miner_common_api.ApiResult_RESULT_ERR_NOT_FOUND,
			Message: fmt.Sprintf("pool with index %d not found", poolToEdit.Idx),
		}), nil
	}

	log.Printf("Pool edited: idx=%d, url=%s (SN: %s)", poolToEdit.Idx, poolToEdit.Url, h.state.SerialNumber)

	return connect.NewResponse(&miner_command_api.CommandResponse{
		Result:  miner_common_api.ApiResult_RESULT_SUCCESS,
		Message: "pool updated",
	}), nil
}

// PlayLocateSequence triggers the LED locate sequence.
func (h *CommandApiHandler) PlayLocateSequence(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_common_api.ApiResultResponse], error) {
	h.state.SetLocateActive(true)
	log.Printf("Locate sequence started (SN: %s)", h.state.SerialNumber)

	return connect.NewResponse(&miner_common_api.ApiResultResponse{
		Result: miner_common_api.ApiResult_RESULT_SUCCESS,
	}), nil
}

// StopLocateSequence stops the LED locate sequence.
func (h *CommandApiHandler) StopLocateSequence(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_common_api.ApiResultResponse], error) {
	h.state.SetLocateActive(false)
	log.Printf("Locate sequence stopped (SN: %s)", h.state.SerialNumber)

	return connect.NewResponse(&miner_common_api.ApiResultResponse{
		Result: miner_common_api.ApiResult_RESULT_SUCCESS,
	}), nil
}

// SetPerformanceTuningAlgorithm sets the hashboard performance tuning algorithm.
func (h *CommandApiHandler) SetPerformanceTuningAlgorithm(ctx context.Context, req *connect.Request[miner_command_api.PerformanceTuningAlgorithmRequest]) (*connect.Response[miner_common_api.ApiResultResponse], error) {
	algo := req.Msg.TuningAlgorithm
	h.state.SetTuningAlgorithm(algo)
	log.Printf("Tuning algorithm set: %v (SN: %s)", algo, h.state.SerialNumber)

	return connect.NewResponse(&miner_common_api.ApiResultResponse{
		Result: miner_common_api.ApiResult_RESULT_SUCCESS,
	}), nil
}
