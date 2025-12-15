// Copyright 2025 Block, Inc.

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"

	common "github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	debugconnect "github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_debug_api/miner_debug_apiconnect"
	errorcode "github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_error_code"
	fanapi "github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_fan_api"
	hbapi "github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_hb_api"
	psuapi "github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_psu_api"
)

const (
	defaultTimeout = 10 * time.Second

	// HTTP/2 transport limits for miner communication
	maxHeaderListSizeMB = 10 // Maximum size of header list in megabytes
	maxReadFrameSizeMB  = 1  // Maximum size of read frame in megabytes
)

type config struct {
	minerAddr string
	action    string
	errorCode string
	index     uint32
	help      bool
}

func main() {
	cfg := parseFlags()

	if cfg.help {
		printHelp()
		os.Exit(0)
	}

	if err := validateConfig(cfg); err != nil {
		log.Fatalf("Configuration error: %v\n\nUse -help for usage information", err)
	}

	if err := run(cfg); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func parseFlags() config {
	cfg := config{}

	flag.StringVar(&cfg.minerAddr, "addr", "localhost:2122", "Miner address (host:port)")
	flag.StringVar(&cfg.action, "action", "", "Action to perform: 'inject' or 'resolve'")
	flag.StringVar(&cfg.errorCode, "error", "", "Error code")

	var indexUint uint
	flag.UintVar(&indexUint, "index", 1, "Component index (0-based, optional)")
	flag.BoolVar(&cfg.help, "help", false, "Show help information")

	flag.Parse()
	cfg.index = uint32(indexUint) // #nosec G115 -- User input, bounded by uint type
	return cfg
}

func validateConfig(cfg config) error {
	if cfg.action == "" {
		return fmt.Errorf("action is required")
	}
	if cfg.action != "inject" && cfg.action != "resolve" {
		return fmt.Errorf("action must be 'inject' or 'resolve', got: %s", cfg.action)
	}
	if cfg.errorCode == "" {
		return fmt.Errorf("error code is required")
	}

	return nil
}

func run(cfg config) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	baseURL := fmt.Sprintf("http://%s", cfg.minerAddr)

	transport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
			return net.Dial(network, addr)
		},
		MaxHeaderListSize:          maxHeaderListSizeMB << 20,
		MaxReadFrameSize:           maxReadFrameSizeMB << 20,
		StrictMaxConcurrentStreams: false,
	}

	httpClient := &http.Client{
		Transport: transport,
	}

	client := debugconnect.NewMinerDebugApiClient(
		httpClient,
		baseURL,
		connect.WithGRPC(),
	)

	errorMsg, err := buildErrorMessage(cfg)
	if err != nil {
		return fmt.Errorf("failed to build error message: %w", err)
	}

	log.Printf("%s error: %s (index: %d)",
		map[string]string{"inject": "Injecting", "resolve": "Resolving"}[cfg.action],
		cfg.errorCode, cfg.index)

	var resp *connect.Response[common.ApiResultResponse]

	if cfg.action == "inject" {
		resp, err = client.CreateMinerNotificationEvent(ctx, connect.NewRequest(errorMsg))
	} else {
		resp, err = client.ClearMinerNotificationEvent(ctx, connect.NewRequest(errorMsg))
	}

	if err != nil {
		return fmt.Errorf("%s failed: %w", cfg.action, err)
	}

	if resp.Msg.Result == common.ApiResult_RESULT_SUCCESS {
		log.Printf("✓ Success: %s completed", cfg.action)
		return nil
	}

	return fmt.Errorf("API returned: %s", resp.Msg.Result.String())
}

type errorBuilder struct {
	prefix     string
	buildError func(code string, index uint32) (*errorcode.Error, error)
}

func buildErrorMessage(cfg config) (*errorcode.Error, error) {
	code := strings.ToUpper(strings.TrimSpace(cfg.errorCode))

	builders := []errorBuilder{
		{"PSU_ERROR_CODE_", buildPsuError},
		{"FAN_ERROR_CODE_", buildFanError},
		{"HB_ERROR_CODE_", buildHbError},
		{"RIG_ERROR_CODE_", buildRigError},
	}

	for _, builder := range builders {
		if strings.HasPrefix(code, builder.prefix) {
			return builder.buildError(code, cfg.index)
		}
	}

	return nil, fmt.Errorf("error code must start with PSU_ERROR_CODE_, FAN_ERROR_CODE_, HB_ERROR_CODE_, or RIG_ERROR_CODE_ prefix (got: %s)", cfg.errorCode)
}

func buildPsuError(code string, index uint32) (*errorcode.Error, error) {
	codeValue, ok := psuapi.PsuErrorCode_value[code]
	if !ok {
		return nil, fmt.Errorf("unknown PSU error code: %s", code)
	}
	return &errorcode.Error{
		Err: &errorcode.Error_PsuError{
			PsuError: &psuapi.PsuError{
				Code:  psuapi.PsuErrorCode(codeValue),
				Index: index,
			},
		},
	}, nil
}

func buildFanError(code string, index uint32) (*errorcode.Error, error) {
	codeValue, ok := fanapi.FanErrorCode_value[code]
	if !ok {
		return nil, fmt.Errorf("unknown Fan error code: %s", code)
	}
	return &errorcode.Error{
		Err: &errorcode.Error_FanError{
			FanError: &fanapi.FanError{
				Code:  fanapi.FanErrorCode(codeValue),
				Index: index,
			},
		},
	}, nil
}

func buildHbError(code string, index uint32) (*errorcode.Error, error) {
	codeValue, ok := hbapi.HbErrorCode_value[code]
	if !ok {
		return nil, fmt.Errorf("unknown Hashboard error code: %s", code)
	}
	return &errorcode.Error{
		Err: &errorcode.Error_HbError{
			HbError: &hbapi.HbError{
				Code:  hbapi.HbErrorCode(codeValue),
				Index: index,
			},
		},
	}, nil
}

func buildRigError(code string, index uint32) (*errorcode.Error, error) {
	codeValue, ok := errorcode.RigErrorCode_value[code]
	if !ok {
		return nil, fmt.Errorf("unknown Rig error code: %s", code)
	}
	rigCode := errorcode.RigErrorCode(codeValue)
	rigError := &errorcode.RigError{
		Code: rigCode,
	}

	needsBayIndex := map[errorcode.RigErrorCode]bool{
		errorcode.RigErrorCode_RIG_ERROR_CODE_INSUFFICIENT_COOLING:     true,
		errorcode.RigErrorCode_RIG_ERROR_CODE_PSU_RECOVERY_IN_PROGRESS: true,
	}
	if needsBayIndex[rigCode] {
		rigError.Detail = &errorcode.RigError_BayIndex_{
			BayIndex: &errorcode.RigError_BayIndex{
				BayIndex: index,
			},
		}
	}

	return &errorcode.Error{
		Err: &errorcode.Error_RigError{
			RigError: rigError,
		},
	}, nil
}

func printHelp() {
	fmt.Print(`miner-debug-cli - Inject or resolve errors on Proto miners via Connect-RPC Debug API
USAGE:
    miner-debug-cli -action <inject|resolve> -error <error_code> [options]

REQUIRED FLAGS:
    -action string
        Action to perform: "inject" (create error) or "resolve" (clear error)

    -error string
        Full protobuf error code name (case-insensitive)
        Must start with: PSU_ERROR_CODE_, FAN_ERROR_CODE_, HB_ERROR_CODE_, or RIG_ERROR_CODE_
        Examples: PSU_ERROR_CODE_OVER_TEMPERATURE, FAN_ERROR_CODE_HARDWARE, HB_ERROR_CODE_COMMUNICATION

OPTIONS:
    -addr string
        Miner address in host:port format (default: "localhost:2122")

    -index uint
        Component index (0-based, default: 1)
        - For PSU errors: PSU unit index (0, 1, 2, ...)
        - For Fan errors: Fan index (0, 1, 2, ...)
        - For Hashboard errors: Hashboard slot index (0, 1, 2, ...)
        - For Rig errors: Bay index (only for INSUFFICIENT_COOLING and PSU_RECOVERY_IN_PROGRESS)

    -help
        Show this help message

AVAILABLE ERROR CODES:

  PSU Errors (prefix: PSU_ERROR_CODE_):
    COMM_LOST, UNDER_VOLTAGE, OVER_VOLTAGE, OUTPUT_FAILURE, OVER_CURRENT, FANS,
    OVER_TEMPERATURE, UNDER_TEMPERATURE, INPUT, NO_INPUT_VOLTAGE, POWER_NO_GOOD

  Fan Errors (prefix: FAN_ERROR_CODE_):
    HARDWARE, SLOW_SPIN, SET_FAN_SPEED_FAILED, FAN_CONNECTED_IN_IMMERSION

  Hashboard Errors (prefix: HB_ERROR_CODE_):
    OVER_HEAT, ASIC_ENUMERATION, COMMUNICATION, ASIC_ECC, UNDER_VOLTAGE, OVER_VOLTAGE,
    OVER_CURRENT, ASIC_OVER_HEAT, ASIC_UNDER_HEAT, ASIC_NOT_HASHING, POWER_LOST

  Rig Errors (prefix: RIG_ERROR_CODE_):
    LOW_HASH_RATE, OVER_HEAT, INSUFFICIENT_COOLING, POOL_CONNECTION_FAILURE,
    POOL_CONFIG_MISSING, MINING_STOPPED_DUE_TO_PHASE_IMBALANCE, PSU_RECOVERY_IN_PROGRESS,
    NETWORK_ERROR, FIRMWARE_UPDATE_FAILURE

EXAMPLES:

  Inject PSU over-temperature error on PSU 0:
    $ miner-debug-cli -action inject -error PSU_ERROR_CODE_OVER_TEMPERATURE -index 0

  Inject fan hardware error on Fan 1:
    $ miner-debug-cli -action inject -error FAN_ERROR_CODE_HARDWARE -index 1

  Inject hashboard communication error on Hashboard 2:
    $ miner-debug-cli -action inject -error HB_ERROR_CODE_COMMUNICATION -index 2

  Inject rig-level pool connection failure:
    $ miner-debug-cli -action inject -error RIG_ERROR_CODE_POOL_CONNECTION_FAILURE

  Resolve PSU over-temperature error:
    $ miner-debug-cli -action resolve -error PSU_ERROR_CODE_OVER_TEMPERATURE -index 0
`)
}
