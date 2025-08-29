package integrationtesting

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api/miner_system_apiconnect"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_command_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_command_api/miner_command_apiconnect"
	minercommonapi "github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
)

type MethodName string

// MethodName represents the name of a method in the API
const (
	MethodStartMining        MethodName = "StartMining"
	MethodStopMining         MethodName = "StopMining"
	MethodSetPowerTarget     MethodName = "SetPowerTarget"
	MethodSetCoolingMode     MethodName = "SetCoolingMode"
	MethodAddPools           MethodName = "AddPools"
	MethodRemovePools        MethodName = "RemovePools"
	MethodEditPool           MethodName = "EditPool"
	MethodGetPairingInfo     MethodName = "GetPairingInfo"
	MethodGetNetwork         MethodName = "GetNetwork"
	MethodSetNetwork         MethodName = "SetNetwork"
	MethodPlayLocateSequence MethodName = "PlayLocateSequence"
	MethodStopLocateSequence MethodName = "StopLocateSequence"
	MethodSetAuthKey         MethodName = "SetAuthKey"
	MethodReboot             MethodName = "Reboot"
	MethodGetLogs            MethodName = "GetLogs"
	MethodInstall            MethodName = "Install"
	MethodUpdate             MethodName = "Update"
	MethodUpload             MethodName = "Upload"
	MethodFactoryReset       MethodName = "FactoryReset"
	MethodClearUserSettings  MethodName = "ClearUserSettings"
)

// MockMinerCallCounter tracks call counts for different API methods
type MockMinerCallCounter struct {
	counts map[MethodName]*atomic.Int32
}

func NewMockMinerCallCounter() *MockMinerCallCounter {
	counter := &MockMinerCallCounter{
		counts: make(map[MethodName]*atomic.Int32),
	}

	methods := []MethodName{
		MethodStartMining,
		MethodStopMining,
		MethodSetPowerTarget,
		MethodSetCoolingMode,
		MethodAddPools,
		MethodRemovePools,
		MethodEditPool,
		MethodGetPairingInfo,
		MethodGetNetwork,
		MethodSetNetwork,
		MethodPlayLocateSequence,
		MethodStopLocateSequence,
		MethodSetAuthKey,
		MethodReboot,
		MethodGetLogs,
		MethodInstall,
		MethodUpdate,
		MethodUpload,
		MethodFactoryReset,
		MethodClearUserSettings,
	}

	for _, method := range methods {
		counter.counts[method] = &atomic.Int32{}
	}

	return counter
}

func (c *MockMinerCallCounter) GetCounter(method MethodName) *atomic.Int32 {
	return c.counts[method]
}

func (c *MockMinerCallCounter) GetCount(method MethodName) int32 {
	return c.GetCounter(method).Load()
}

func (c *MockMinerCallCounter) AssertCalls(t *testing.T, method MethodName, expectedCount int32) {
	actualCount := c.GetCount(method)
	assert.Equal(t, expectedCount, actualCount,
		"Expected %s to be called exactly %d times, got %d",
		method, expectedCount, actualCount)
}

func handleRequestUnauthenticated[Req, Resp any](
	t *testing.T,
	methodName string,
	req *connect.Request[Req],
	counter *atomic.Int32,
	handler func(*Req) *Resp,
) *connect.Response[Resp] {
	t.Logf("Mock miner received %s request with headers: %v", methodName, req.Header())

	counter.Add(1)
	return connect.NewResponse(handler(req.Msg))
}

func handleRequest[Req, Resp any](
	t *testing.T,
	methodName string,
	req *connect.Request[Req],
	counter *atomic.Int32,
	handler func(*Req) *Resp,
) (*connect.Response[Resp], error) {
	if req.Header().Get("Authorization") == "" {
		return nil, fleeterror.NewUnauthenticatedError("expected Authorization header")
	}

	return handleRequestUnauthenticated(t, methodName, req, counter, handler), nil
}

func handleCommandRequest[Req any](
	t *testing.T,
	methodName string,
	req *connect.Request[Req],
	counter *atomic.Int32,
	successMessage string,
) (*connect.Response[miner_command_api.CommandResponse], error) {
	return handleRequest(
		t, methodName, req, counter,
		func(_ *Req) *miner_command_api.CommandResponse {
			return &miner_command_api.CommandResponse{
				Result:  minercommonapi.ApiResult_RESULT_SUCCESS,
				Message: successMessage,
			}
		})
}

type MockMinerHandler struct {
	t           *testing.T
	callCounter *MockMinerCallCounter
}

var _ miner_command_apiconnect.MinerCommandApiHandler = &MockMinerHandler{}
var _ miner_system_apiconnect.MinerSystemApiHandler = &MockMinerHandler{}
var _ miner_system_apiconnect.MinerPairingApiHandler = &MockMinerHandler{}

func NewMockMinerHandler(t *testing.T, callCounter *MockMinerCallCounter) *MockMinerHandler {
	return &MockMinerHandler{
		t:           t,
		callCounter: callCounter,
	}
}

func (m *MockMinerHandler) StartMining(ctx context.Context, req *connect.Request[minercommonapi.EmptyRequest]) (*connect.Response[miner_command_api.CommandResponse], error) {
	return handleCommandRequest(m.t, "StartMining", req, m.callCounter.GetCounter(MethodStartMining), "Mining started successfully")
}

func (m *MockMinerHandler) StopMining(ctx context.Context, req *connect.Request[minercommonapi.EmptyRequest]) (*connect.Response[miner_command_api.CommandResponse], error) {
	return handleCommandRequest(m.t, "StopMining", req, m.callCounter.GetCounter(MethodStopMining), "Mining stopped successfully")
}

func (m *MockMinerHandler) SetPowerTarget(ctx context.Context, req *connect.Request[miner_command_api.PowerTargetRequest]) (*connect.Response[miner_data_api.PowerTargetResponse], error) {
	return handleRequest(
		m.t, "SetPowerTarget", req, m.callCounter.GetCounter(MethodSetPowerTarget),
		func(msg *miner_command_api.PowerTargetRequest) *miner_data_api.PowerTargetResponse {
			return &miner_data_api.PowerTargetResponse{
				Result:       minercommonapi.ApiResult_RESULT_SUCCESS,
				PowerTargetW: msg.PowerTargetW,
			}
		})
}

func (m *MockMinerHandler) SetCoolingMode(ctx context.Context, req *connect.Request[miner_command_api.CoolingModeRequest]) (*connect.Response[miner_data_api.CoolingModeResponse], error) {
	return handleRequest(
		m.t, "SetCoolingMode", req, m.callCounter.GetCounter(MethodSetCoolingMode),
		func(msg *miner_command_api.CoolingModeRequest) *miner_data_api.CoolingModeResponse {
			return &miner_data_api.CoolingModeResponse{
				Result: minercommonapi.ApiResult_RESULT_SUCCESS,
				Mode:   msg.Mode,
			}
		})
}

func (m *MockMinerHandler) AddPools(ctx context.Context, req *connect.Request[miner_command_api.PoolsRequest]) (*connect.Response[miner_command_api.CommandResponse], error) {
	return handleCommandRequest(m.t, "AddPools", req, m.callCounter.GetCounter(MethodAddPools), "Pools added successfully")
}

func (m *MockMinerHandler) RemovePools(ctx context.Context, req *connect.Request[miner_command_api.PoolsRequest]) (*connect.Response[miner_command_api.CommandResponse], error) {
	return handleCommandRequest(m.t, "RemovePools", req, m.callCounter.GetCounter(MethodRemovePools), "Pools removed successfully")
}

func (m *MockMinerHandler) EditPool(ctx context.Context, req *connect.Request[miner_data_api.Pool]) (*connect.Response[miner_command_api.CommandResponse], error) {
	return handleCommandRequest(m.t, "EditPool", req, m.callCounter.GetCounter(MethodEditPool), "Pool edited successfully")
}

func (m *MockMinerHandler) GetPairingInfo(ctx context.Context, req *connect.Request[minercommonapi.EmptyRequest]) (*connect.Response[miner_system_api.GetPairingInfoResponse], error) {
	return handleRequestUnauthenticated(
		m.t, "GetPairingInfo", req, m.callCounter.GetCounter(MethodGetPairingInfo),
		func(_ *minercommonapi.EmptyRequest) *miner_system_api.GetPairingInfoResponse {
			return &miner_system_api.GetPairingInfoResponse{
				Mac:  "00:00:00:00:00:00",
				CbSn: "1234567890",
			}
		}), nil
}

func (m *MockMinerHandler) GetNetwork(ctx context.Context, req *connect.Request[minercommonapi.EmptyRequest]) (*connect.Response[miner_system_api.GetNetworkResponse], error) {
	return handleRequestUnauthenticated(
		m.t, "GetNetwork", req, m.callCounter.GetCounter(MethodGetNetwork),
		func(_ *minercommonapi.EmptyRequest) *miner_system_api.GetNetworkResponse {
			return &miner_system_api.GetNetworkResponse{}
		}), nil
}

func (m *MockMinerHandler) SetNetwork(ctx context.Context, req *connect.Request[miner_system_api.SetNetworkRequest]) (*connect.Response[miner_system_api.SetNetworkResponse], error) {
	return handleRequest(
		m.t, "SetNetwork", req, m.callCounter.GetCounter(MethodSetNetwork),
		func(_ *miner_system_api.SetNetworkRequest) *miner_system_api.SetNetworkResponse {
			return &miner_system_api.SetNetworkResponse{}
		})
}

func (m *MockMinerHandler) PlayLocateSequence(_ context.Context, req *connect.Request[minercommonapi.EmptyRequest]) (*connect.Response[minercommonapi.ApiResultResponse], error) {
	return handleRequest(
		m.t, "PlayLocateSequence", req, m.callCounter.GetCounter(MethodPlayLocateSequence),
		func(_ *minercommonapi.EmptyRequest) *minercommonapi.ApiResultResponse {
			return &minercommonapi.ApiResultResponse{Result: minercommonapi.ApiResult_RESULT_SUCCESS}
		},
	)
}

func (m *MockMinerHandler) StopLocateSequence(_ context.Context, req *connect.Request[minercommonapi.EmptyRequest]) (*connect.Response[minercommonapi.ApiResultResponse], error) {
	return handleRequest(
		m.t, "StopLocateSequence", req, m.callCounter.GetCounter(MethodStopLocateSequence),
		func(_ *minercommonapi.EmptyRequest) *minercommonapi.ApiResultResponse {
			return &minercommonapi.ApiResultResponse{Result: minercommonapi.ApiResult_RESULT_SUCCESS}
		},
	)
}

func (m *MockMinerHandler) SetAuthKey(ctx context.Context, req *connect.Request[miner_system_api.SetAuthKeyRequest]) (*connect.Response[miner_system_api.SetAuthKeyResponse], error) {
	return handleRequestUnauthenticated(
		m.t,
		"SetAuthKey",
		req,
		m.callCounter.GetCounter(MethodSetAuthKey),
		func(_ *miner_system_api.SetAuthKeyRequest) *miner_system_api.SetAuthKeyResponse {
			return &miner_system_api.SetAuthKeyResponse{
				Message: "Auth key set successfully",
			}
		}), nil
}

func (m *MockMinerHandler) Reboot(ctx context.Context, req *connect.Request[minercommonapi.EmptyRequest]) (*connect.Response[minercommonapi.ApiResultResponse], error) {
	return handleRequest(
		m.t, "Reboot", req, m.callCounter.GetCounter(MethodReboot),
		func(_ *minercommonapi.EmptyRequest) *minercommonapi.ApiResultResponse {
			return &minercommonapi.ApiResultResponse{
				Result: minercommonapi.ApiResult_RESULT_SUCCESS,
			}
		})
}

func (m *MockMinerHandler) GetLogs(ctx context.Context, req *connect.Request[miner_system_api.GetLogsRequest]) (*connect.Response[miner_system_api.GetLogsResponse], error) {
	return handleRequest(
		m.t, "GetLogs", req, m.callCounter.GetCounter(MethodGetLogs),
		func(_ *miner_system_api.GetLogsRequest) *miner_system_api.GetLogsResponse {
			return &miner_system_api.GetLogsResponse{
				Source:  req.Msg.Source,
				Lines:   3,
				Content: []string{"Mock log data", "Line 2", "Line 3"},
			}
		})
}

func (m *MockMinerHandler) Install(ctx context.Context, req *connect.Request[minercommonapi.EmptyRequest]) (*connect.Response[minercommonapi.ApiResultResponse], error) {
	return handleRequest(
		m.t, "Install", req, m.callCounter.GetCounter(MethodInstall),
		func(_ *minercommonapi.EmptyRequest) *minercommonapi.ApiResultResponse {
			return &minercommonapi.ApiResultResponse{
				Result: minercommonapi.ApiResult_RESULT_SUCCESS,
			}
		})
}

func (m *MockMinerHandler) Update(ctx context.Context, req *connect.Request[minercommonapi.EmptyRequest]) (*connect.Response[miner_system_api.UpdateResponse], error) {
	return handleRequest(
		m.t, "Update", req, m.callCounter.GetCounter(MethodUpdate),
		func(_ *minercommonapi.EmptyRequest) *miner_system_api.UpdateResponse {
			return &miner_system_api.UpdateResponse{
				Message: "Update installation started.",
			}
		})
}

func (m *MockMinerHandler) Upload(ctx context.Context, _ *connect.ClientStream[miner_system_api.UploadRequest]) (*connect.Response[miner_system_api.UploadResponse], error) {
	return connect.NewResponse(&miner_system_api.UploadResponse{
		Message: "Update file uploaded successfully.",
	}), nil
}

func (m *MockMinerHandler) FactoryReset(ctx context.Context, req *connect.Request[minercommonapi.EmptyRequest]) (*connect.Response[minercommonapi.ApiResultResponse], error) {
	return handleRequest(
		m.t, "FactoryReset", req, m.callCounter.GetCounter(MethodFactoryReset),
		func(_ *minercommonapi.EmptyRequest) *minercommonapi.ApiResultResponse {
			return &minercommonapi.ApiResultResponse{
				Result: minercommonapi.ApiResult_RESULT_SUCCESS,
			}
		})
}

func (m *MockMinerHandler) ClearUserSettings(ctx context.Context, req *connect.Request[minercommonapi.EmptyRequest]) (*connect.Response[minercommonapi.ApiResultResponse], error) {
	return handleRequest(
		m.t, "ClearUserSettings", req, m.callCounter.GetCounter(MethodClearUserSettings),
		func(_ *minercommonapi.EmptyRequest) *minercommonapi.ApiResultResponse {
			return &minercommonapi.ApiResultResponse{
				Result: minercommonapi.ApiResult_RESULT_SUCCESS,
			}
		})
}
