package rpc_test

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"testing"

	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/networking"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockRPCServer struct {
	listener  net.Listener
	responses map[string]string
}

func NewMockRPCServer() (*MockRPCServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %v", err)
	}

	server := &MockRPCServer{
		listener:  listener,
		responses: make(map[string]string),
	}

	server.setupMockResponses()

	go server.serve()
	return server, nil
}

func (s *MockRPCServer) setupMockResponses() {
	s.responses["version"] = `
	{
		"STATUS": [
			{
			"STATUS": "S",
			"When": 1750277143,
			"Code": 22,
			"Msg": "CGMiner versions",
			"Description": "cgminer 1.0.0"
			}
		],
		"VERSION": [
			{
			"BMMiner": "1.0.0",
			"API": "3.1",
			"Miner": "uart_trans.1.3",
			"CompileTime": "Thu Jul 11 16:38:25 CST 2024",
			"Type": "Antminer S21"
			}
		],
		"id": 1
	}
	`

	s.responses["summary"] = `
	{
		"STATUS": [
			{
			"STATUS": "S",
			"When": 1750277145,
			"Code": 11,
			"Msg": "Summary",
			"Description": "cgminer 1.0.0"
			}
		],
		"SUMMARY": [
			{
			"Elapsed": 59129,
			"GHS 5s": 203007.01,
			"GHS av": 203675.66,
			"GHS 30m": 203915.73,
			"Found Blocks": 0,
			"Getwork": 6033,
			"Accepted": 11187,
			"Rejected": 9,
			"Hardware Errors": 61,
			"Utility": 11.35,
			"Discarded": 5554970,
			"Stale": 0,
			"Get Failures": 0,
			"Local Work": 5560881,
			"Remote Failures": 0,
			"Network Blocks": 102,
			"Total MH": 1.2009939119E+13,
			"Work Utility": 2829363.48,
			"Difficulty Accepted": 2785931264.0,
			"Difficulty Rejected": 2359296.0,
			"Difficulty Stale": 0.0,
			"Best Share": 5254813688,
			"Device Hardware%": 0.0,
			"Device Rejected%": 0.0,
			"Pool Rejected%": 0.0,
			"Pool Stale%": 0.0,
			"Last getwork": 1750277145
			}
		],
		"id": 1
	}
	`

	s.responses["pools"] = `
	{
		"STATUS": [
			{
			"STATUS": "S",
			"When": 1750277146,
			"Code": 7,
			"Msg": "3 Pool(s)",
			"Description": "cgminer 1.0.0"
			}
		],
		"POOLS": [
			{
			"POOL": 0,
			"URL": "stratum+tcp://stratum.braiins.com:3333",
			"Status": "Alive",
			"Priority": 0,
			"Quota": 1,
			"Long Poll": "N",
			"Getworks": 6033,
			"Accepted": 11187,
			"Rejected": 9,
			"Discarded": 5555071,
			"Stale": 0,
			"Get Failures": 0,
			"Remote Failures": 0,
			"User": "proto_mining_sw_test_s21-0a5",
			"Last Share Time": "0:00:12",
			"Diff": "262K",
			"Diff1 Shares": 0,
			"Proxy Type": "",
			"Proxy": "",
			"Difficulty Accepted": 2785931264.0,
			"Difficulty Rejected": 2359296.0,
			"Difficulty Stale": 0.0,
			"Last Share Difficulty": 262144.0,
			"Has Stratum": true,
			"Stratum Active": true,
			"Stratum URL": "stratum.braiins.com",
			"Has GBT": false,
			"Best Share": 5254813688.0,
			"Pool Rejected%": 0.0,
			"Pool Stale%%": 0.0
			},
			{
			"POOL": 1,
			"URL": "",
			"Status": "Deed",
			"Priority": 1,
			"Quota": 1,
			"Long Poll": "N",
			"Getworks": 0,
			"Accepted": 0,
			"Rejected": 0,
			"Discarded": 0,
			"Stale": 0,
			"Get Failures": 0,
			"Remote Failures": 0,
			"User": "",
			"Last Share Time": "0",
			"Diff": "",
			"Diff1 Shares": 0,
			"Proxy Type": "",
			"Proxy": "",
			"Difficulty Accepted": 0.0,
			"Difficulty Rejected": 0.0,
			"Difficulty Stale": 0.0,
			"Last Share Difficulty": 0.0,
			"Has Stratum": false,
			"Stratum Active": false,
			"Stratum URL": "",
			"Has GBT": false,
			"Best Share": 0.0,
			"Pool Rejected%": 0.0,
			"Pool Stale%%": 0.0
			},
			{
			"POOL": 2,
			"URL": "",
			"Status": "Deed",
			"Priority": 2,
			"Quota": 1,
			"Long Poll": "N",
			"Getworks": 0,
			"Accepted": 0,
			"Rejected": 0,
			"Discarded": 0,
			"Stale": 0,
			"Get Failures": 0,
			"Remote Failures": 0,
			"User": "",
			"Last Share Time": "0",
			"Diff": "",
			"Diff1 Shares": 0,
			"Proxy Type": "",
			"Proxy": "",
			"Difficulty Accepted": 0.0,
			"Difficulty Rejected": 0.0,
			"Difficulty Stale": 0.0,
			"Last Share Difficulty": 0.0,
			"Has Stratum": false,
			"Stratum Active": false,
			"Stratum URL": "",
			"Has GBT": false,
			"Best Share": 0.0,
			"Pool Rejected%": 0.0,
			"Pool Stale%%": 0.0
			}
		],
		"id": 1
	}
	`

	s.responses["devs"] = `
	{
		"STATUS": [
			{
			"STATUS": "S",
			"When": 1750277147,
			"Code": 9,
			"Msg": "1 ASC(s)",
			"Description": "cgminer 1.0.0"
			}
		],
		"DEVS": [
			{
			"ASC": 0,
			"Name": "BTM_SOC",
			"ID": 0,
			"Enabled": "Y",
			"Status": "Alive",
			"Temperature": 65.0,
			"MHS av": 203676.57,
			"MHS 5s": 202266.83,
			"Accepted": 11188,
			"Rejected": 9,
			"Hardware Errors": 0,
			"Utility": 0.0,
			"Last Share Pool": 0,
			"Last Share Time": 1750277146,
			"Total MH": 0.0,
			"Diff1 Work": 0,
			"Difficulty Accepted": 2786193408,
			"Difficulty Rejected": 2359296,
			"Last Share Difficulty": 1750277146,
			"Last Valid Work": 1750277146,
			"Device Hardware%": 0.0,
			"Device Rejected%": 0.0,
			"Device Elapsed": 59131
			}
		],
		"id": 1
	}
	`

	s.responses["stats"] = `
	{
		"STATUS": [
			{
			"STATUS": "S",
			"When": 1750277150,
			"Code": 70,
			"Msg": "CGMiner stats",
			"Description": "cgminer 1.0.0"
			}
		],
		"STATS": [
			{"BMMiner": "1.0.0", "Miner": "49.0.1.3", "CompileTime": "Thu Jul 11 16:38:25 CST 2024", "Type": "Antminer S21"},
			{"STATS": 0, "ID": "BTM_SOC0", "Elapsed": 59129, "GHS 5s": 203007.01, "GHS av": 203675.66, "chain_power": "3250 W", "fan_num": 4, "fan1": 6000, "fan2": 5880, "fan3": 5040, "fan4": 5040}
		],
		"id": 1
	}
	`

	s.responses["config"] = `
	{
		"STATUS": [
			{
			"STATUS": "S",
			"When": 1750277148,
			"Code": 33,
			"Msg": "CGMiner config",
			"Description": "cgminer 1.0.0"
			}
		],
		"CONFIG": [
			{
			"ASC Count": 3,
			"PGA Count": 0,
			"Pool Count": 3,
			"Strategy": "Failover",
			"Log Interval": 5,
			"Device Code": "BTM_SOC",
			"OS": "Linux",
			"Failover-Only": true,
			"ScanTime": 60,
			"Queue": 1,
			"Expiry": 120
			}
		],
		"id": 1
	}
	`
}

func (s *MockRPCServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConnection(conn)
	}
}

func (s *MockRPCServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil {
		return
	}

	var request rpc.RPCRequest
	if err := json.Unmarshal(buffer[:n], &request); err != nil {
		return
	}

	response, exists := s.responses[request.Command]
	if !exists {
		response = `
		{
			"STATUS": [
				{
				"STATUS": "E",
				"When": 1750277148,
				"Code": 14,
				"Msg": "Invalid command",
				"Description": "cgminer 1.0.0"
				}
			],
			"id": 1
		}
		`
	}

	_, err = conn.Write([]byte(response))
	if err != nil {
		return
	}
}

func (s *MockRPCServer) GetAddress() string {
	return s.listener.Addr().String()
}

func (s *MockRPCServer) Close() {
	s.listener.Close()
}

func TestRPCCommands(t *testing.T) {
	server, err := NewMockRPCServer()
	require.NoError(t, err, "Failed to create mock server")
	defer server.Close()

	host, portStr, err := net.SplitHostPort(server.GetAddress())
	require.NoError(t, err, "Failed to split host port")
	portInt, err := strconv.Atoi(portStr)
	require.NoError(t, err, "Failed to convert port to int")

	rpcClient := rpc.NewService()
	connInfo := &networking.ConnectionInfo{
		IPAddress: networking.IPAddress(host),
		Port:      networking.Port(portInt), //nolint:gosec // This is a test
		Protocol:  networking.ProtocolTCP,
	}

	t.Run("GetVersion", func(t *testing.T) {
		response, err := rpcClient.GetVersion(t.Context(), connInfo)
		require.NoError(t, err, "GetVersion should not return error")
		assert.NotZero(t, response, "Response should not be nil")
		assert.Len(t, response.Version, 1, "Should have one version entry")

		version := response.Version[0]
		assert.Equal(t, "1.0.0", version.BMMiner, "BMMiner version should match")
		assert.Equal(t, "3.1", version.API, "API version should match")
		assert.Equal(t, "uart_trans.1.3", version.Miner, "Miner model should match")
		assert.Equal(t, "Thu Jul 11 16:38:25 CST 2024", version.CompileTime, "Compile time should match")
		assert.Equal(t, "Antminer S21", version.Type, "Miner type should match")

		// Check status
		assert.Len(t, response.Status, 1, "Should have one status entry")
		assert.Equal(t, "S", response.Status[0].Status, "Status should be Success")
		assert.Equal(t, "CGMiner versions", response.Status[0].Msg, "Message should match")
	})

	t.Run("GetSummary", func(t *testing.T) {
		response, err := rpcClient.GetSummary(t.Context(), connInfo)
		require.NoError(t, err, "GetSummary should not return error")
		assert.NotZero(t, response, "Response should not be nil")
		assert.Len(t, response.Summary, 1, "Should have one summary entry")

		summary := response.Summary[0]
		assert.Equal(t, int64(59129), summary.Elapsed, "Elapsed time should match")
		assert.InDelta(t, 203007.01, summary.GHS5s, 0.01, "5s hash rate should match")
		assert.InDelta(t, 203675.66, summary.GHSAv, 0.01, "Average hash rate should match")
		assert.Equal(t, int64(11187), summary.Accepted, "Accepted shares should match")
		assert.Equal(t, int64(9), summary.Rejected, "Rejected shares should match")
		assert.Equal(t, int64(61), summary.HardwareErrors, "Hardware errors should match")
		assert.InDelta(t, 11.35, summary.Utility, 0.01, "Utility should match")
		assert.InDelta(t, 2829363.48, summary.WorkUtility, 0.01, "Work utility should match")
	})

	t.Run("GetPools", func(t *testing.T) {
		response, err := rpcClient.GetPools(t.Context(), connInfo)
		require.NoError(t, err, "GetPools should not return error")
		assert.NotZero(t, response, "Response should not be nil")
		assert.Len(t, response.Pools, 3, "Should have 3 pool entries")

		// Test active pool
		activePool := response.Pools[0]
		assert.Equal(t, 0, activePool.Pool, "Pool number should be 0")
		assert.Equal(t, "stratum+tcp://stratum.braiins.com:3333", activePool.URL, "Pool URL should match")
		assert.Equal(t, "proto_mining_sw_test_s21-0a5", activePool.User, "Pool user should match")
		assert.Equal(t, "Alive", activePool.Status, "Pool status should be Alive")
		assert.Equal(t, 0, activePool.Priority, "Pool priority should be 0")
		assert.Equal(t, int64(11187), activePool.Accepted, "Accepted shares should match")
		assert.Equal(t, int64(9), activePool.Rejected, "Rejected shares should match")

		// Test inactive pools
		for i := 1; i < 3; i++ {
			pool := response.Pools[i]
			assert.Equal(t, i, pool.Pool, "Pool number should match index")
			assert.Equal(t, "", pool.URL, "Inactive pool URL should be empty")
			assert.Equal(t, "Deed", pool.Status, "Inactive pool status should be Dead") // Note: typo in actual response
		}
	})

	t.Run("GetDevs", func(t *testing.T) {
		response, err := rpcClient.GetDevs(t.Context(), connInfo)
		require.NoError(t, err, "GetDevs should not return error")
		assert.NotZero(t, response, "Response should not be nil")
		assert.Len(t, response.Devs, 1, "Should have 1 device entry")

		// Test device
		dev := response.Devs[0]
		assert.Equal(t, 0, dev.ASC, "ASC number should be 0")
		assert.Equal(t, "BTM_SOC", dev.Name, "Device name should match")
		assert.Equal(t, 0, dev.ID, "Device ID should be 0")
		assert.Equal(t, "Y", dev.Enabled, "Device should be enabled")
		assert.Equal(t, "Alive", dev.Status, "Device status should be Alive")
		assert.InDelta(t, 65.0, dev.Temperature, 0.01, "Temperature should match")
		assert.InDelta(t, 203676.57, dev.MHSAv, 0.01, "Average MHS should match")
		assert.Equal(t, int64(11188), dev.Accepted, "Accepted shares should match")
		assert.Equal(t, int64(9), dev.Rejected, "Rejected shares should match")
		assert.Equal(t, int64(0), dev.HardwareErrors, "Hardware errors should be 0")
	})

	t.Run("GetStats", func(t *testing.T) {
		response, err := rpcClient.GetStats(t.Context(), connInfo)
		require.NoError(t, err, "GetStats should not return error")
		assert.NotZero(t, response, "Response should not be nil")

		// Check status
		assert.Len(t, response.Status, 1, "Should have one status entry")
		assert.Equal(t, "S", response.Status[0].Status, "Status should be Success")

		// STATS array should have 2 entries (firmware info + mining stats)
		assert.Len(t, response.Stats, 2, "Should have two STATS entries")

		// Parse the second entry to verify chain_power is present
		var statsData map[string]json.RawMessage
		err = json.Unmarshal(response.Stats[1], &statsData)
		require.NoError(t, err, "Should be able to parse STATS[1]")

		_, hasChainPower := statsData["chain_power"]
		assert.True(t, hasChainPower, "STATS[1] should contain chain_power")
	})

	t.Run("GetConfig", func(t *testing.T) {
		response, err := rpcClient.GetConfig(t.Context(), connInfo)
		require.NoError(t, err, "GetConfig should not return error")
		assert.NotZero(t, response, "Response should not be nil")
		assert.Len(t, response.Config, 1, "Should have one config entry")

		config := response.Config[0]
		assert.Equal(t, 3, config.ASCCount, "ASC count should be 3")
		assert.Equal(t, 0, config.PGACount, "PGA count should be 0")
		assert.Equal(t, 3, config.PoolCount, "Pool count should be 3")
		assert.Equal(t, "Failover", config.Strategy, "Strategy should be Failover")
		assert.Equal(t, 5, config.LogInterval, "Log interval should be 5")
		assert.Equal(t, "BTM_SOC", config.DeviceCode, "Device code should match")
		assert.Equal(t, "Linux", config.OS, "OS should be Linux")
	})
}

func TestRPCErrorHandling(t *testing.T) {
	rpcClient := rpc.NewService()
	connInfo := &networking.ConnectionInfo{
		IPAddress: "127.0.0.1",
		Port:      networking.Port(9999), // Non-existent port
	}

	t.Run("ConnectionFailure", func(t *testing.T) {
		_, err := rpcClient.GetVersion(t.Context(), connInfo)
		require.Error(t, err, "Should return error for connection failure")
		assert.Contains(t, err.Error(), "failed to connect", "Error should mention connection failure")
	})
}

// Test invalid command
func TestRPCInvalidCommand(t *testing.T) {
	server, err := NewMockRPCServer()
	require.NoError(t, err, "Failed to create mock server")
	defer server.Close()

	// Test with raw connection to send invalid command
	conn, err := net.Dial("tcp", server.GetAddress())
	require.NoError(t, err, "Failed to connect to mock server")
	defer conn.Close()

	// Send invalid command
	invalidRequest := `{"command": "invalid_command"}`
	n, err := conn.Write([]byte(invalidRequest))
	require.NoError(t, err, "Failed to write invalid request")
	assert.Positive(t, n, "Should write some bytes")

	// Read response
	buffer := make([]byte, 1024)
	n, err = conn.Read(buffer)
	require.NoError(t, err, "Failed to read response")

	response := string(buffer[:n])
	assert.Contains(t, response, "Invalid command", "Response should contain error message")
}
