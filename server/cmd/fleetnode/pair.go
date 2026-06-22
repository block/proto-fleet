package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const pairConcurrency = 16

// Mirror the FleetNodePairResult string caps so a plugin returning an oversized
// identity field can't fail validation for the whole ReportPairedDevices chunk
// (which would drop every other device's outcome in that batch).
const (
	maxPairIdentityBytes = 255
	maxPairMACBytes      = 64
)

// perPairTimeout bounds one device's auth handshake. var so tests can shrink it.
var perPairTimeout = 60 * time.Second

// pairer authenticates one discovered device and returns its per-device result.
// It never returns an error: auth and plugin failures map to a PairOutcome so a
// single bad device never fails the batch.
type pairer interface {
	Pair(ctx context.Context, target *pairingpb.FleetNodePairTarget, creds *pairingpb.Credentials) *pb.FleetNodePairResult
}

type pluginPairer struct {
	manager *plugins.Manager
}

func newPluginPairer(manager *plugins.Manager, _ string) (*pluginPairer, error) {
	return &pluginPairer{manager: manager}, nil
}

func (p *pluginPairer) Pair(ctx context.Context, target *pairingpb.FleetNodePairTarget, creds *pairingpb.Credentials) *pb.FleetNodePairResult {
	res := &pb.FleetNodePairResult{DeviceIdentifier: target.GetDeviceIdentifier()}

	plugin, err := p.manager.GetPluginByDriverNameWithCapability(target.GetDriverName(), sdk.CapabilityPairing)
	if err != nil {
		res.Outcome = pb.PairOutcome_PAIR_OUTCOME_ERROR
		res.ErrorMessage = truncateUTF8(fmt.Sprintf("no pairing-capable driver %q: %v", target.GetDriverName(), err), maxAckErrorMessageBytes)
		return res
	}

	port, err := sdk.ParsePort(target.GetPort())
	if err != nil {
		res.Outcome = pb.PairOutcome_PAIR_OUTCOME_ERROR
		res.ErrorMessage = truncateUTF8(fmt.Sprintf("invalid port %q: %v", target.GetPort(), err), maxAckErrorMessageBytes)
		return res
	}
	deviceInfo := sdk.DeviceInfo{
		Host:            target.GetIpAddress(),
		Port:            port,
		URLScheme:       target.GetUrlScheme(),
		Manufacturer:    target.GetManufacturer(),
		FirmwareVersion: target.GetFirmwareVersion(),
	}

	// Operator-supplied username/password covers basic-auth drivers.
	if bundle, ok := secretBundleFor(creds); ok {
		updated, pairErr := plugin.Driver.PairDevice(ctx, deviceInfo, bundle)
		if pairErr != nil {
			classifyNodePairError(pairErr, res)
			return res
		}
		setPaired(res, updated)
		return res
	}

	// No credentials supplied: try plugin-provided defaults.
	if provider, ok := plugin.Driver.(sdk.DefaultCredentialsProvider); ok {
		defaults := provider.GetDefaultCredentials(ctx, target.GetManufacturer(), target.GetFirmwareVersion())
		for _, c := range defaults {
			bundle := sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Username: c.Username, Password: c.Password}}
			updated, pairErr := plugin.Driver.PairDevice(ctx, deviceInfo, bundle)
			if pairErr != nil {
				if isNodeAuthFailure(pairErr) {
					continue
				}
				classifyNodePairError(pairErr, res)
				return res
			}
			setPaired(res, updated)
			return res
		}
	}

	res.Outcome = pb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED
	res.ErrorMessage = "credentials required for pairing"
	return res
}

// secretBundleFor returns an auth bundle when operator-supplied username/password
// credentials apply. ok is false when the caller should fall back to plugin
// defaults or report AUTH_NEEDED.
func secretBundleFor(creds *pairingpb.Credentials) (sdk.SecretBundle, bool) {
	if creds != nil && creds.Password != nil {
		return sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Username: creds.GetUsername(), Password: creds.GetPassword()}}, true
	}
	return sdk.SecretBundle{}, false
}

func setPaired(res *pb.FleetNodePairResult, info sdk.DeviceInfo) {
	res.Outcome = pb.PairOutcome_PAIR_OUTCOME_PAIRED
	res.SerialNumber = truncateUTF8(info.SerialNumber, maxPairIdentityBytes)
	res.MacAddress = truncateUTF8(info.MacAddress, maxPairMACBytes)
	res.Model = truncateUTF8(info.Model, maxPairIdentityBytes)
	res.Manufacturer = truncateUTF8(info.Manufacturer, maxPairIdentityBytes)
	res.FirmwareVersion = truncateUTF8(info.FirmwareVersion, maxPairIdentityBytes)
}

// classifyNodePairError maps a plugin pairing error to a per-device outcome.
// Authentication failures (credentials rejected) map to AUTH_FAILED so the
// operator can retry with better credentials; everything else is ERROR.
func classifyNodePairError(err error, res *pb.FleetNodePairResult) {
	if isNodeAuthFailure(err) {
		res.Outcome = pb.PairOutcome_PAIR_OUTCOME_AUTH_FAILED
	} else {
		res.Outcome = pb.PairOutcome_PAIR_OUTCOME_ERROR
	}
	res.ErrorMessage = truncateUTF8(err.Error(), maxAckErrorMessageBytes)
}

func isNodeAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	if status.Code(err) == codes.Unauthenticated {
		return true
	}
	var sdkErr sdk.SDKError
	return errors.As(err, &sdkErr) && sdkErr.Code == sdk.ErrCodeAuthenticationFailed
}

// handlePairCommand pairs a batch of discovered devices and streams the results
// back. It mirrors the discovery path: bounded fan-out, chunked report upload,
// PARTIAL on deadline. Per-device outcomes ride the report, not the ack.
func (r *RunCmd) handlePairCommand(ctx context.Context, client gatewayClient, stream acker, commandID string, req *pairingpb.FleetNodePairRequest, logger *slog.Logger) {
	if r.pairer == nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "pairing unavailable: no plugins loaded", logger)
		return
	}
	if vErr := protovalidate.Validate(req); vErr != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, fmt.Sprintf("invalid pair request: %v", vErr), logger)
		return
	}
	targets := req.GetTargets()
	logger.Info("pair command received", "command_id", commandID, "targets", len(targets))

	cmdCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	results, truncated := fanOutPairs(cmdCtx, targets, req.GetCredentials(), pairConcurrency, r.pairer.Pair, logger)

	// Stream on the parent ctx, not cmdCtx: a deadline-hit cmdCtx must not
	// suppress upload of the results already collected.
	if err := r.streamPairResults(ctx, client, commandID, results, logger); err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_REPORT_FAILED, err.Error(), logger)
		return
	}
	if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_PARTIAL, fmt.Sprintf("pairing exceeded command deadline (%s); %d of %d result(s) uploaded", commandTimeout, len(results), len(targets)), logger)
		return
	}
	if truncated {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_PARTIAL, fmt.Sprintf("pair supervisor budget exceeded; %d of %d result(s) uploaded", len(results), len(targets)), logger)
		return
	}
	r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_OK, "", logger)
}

func (r *RunCmd) streamPairResults(ctx context.Context, client gatewayClient, commandID string, results []*pb.FleetNodePairResult, logger *slog.Logger) error {
	for chunk := range slices.Chunk(results, maxDevicesPerReport) {
		callCtx, cancel := context.WithTimeout(ctx, discoveryReportTimeout)
		_, err := client.ReportPairedDevices(callCtx, connect.NewRequest(&pb.ReportPairedDevicesRequest{
			CommandId: commandID,
			Results:   chunk,
		}))
		cancel()
		if err != nil {
			logger.Error("pair report failed", "command_id", commandID, "err", err)
			return fmt.Errorf("report paired devices: %w", err)
		}
		logger.Info("pair report accepted", "command_id", commandID, "batch_size", len(chunk))
	}
	return nil
}

// fanOutPairs pairs targets with bounded concurrency, returning collected
// results and whether the batch was truncated (a hung plugin or a cancelled
// parent ctx left some targets unattempted; the operator re-lists and retries).
func fanOutPairs(ctx context.Context, targets []*pairingpb.FleetNodePairTarget, creds *pairingpb.Credentials, concurrency int, pair func(context.Context, *pairingpb.FleetNodePairTarget, *pairingpb.Credentials) *pb.FleetNodePairResult, logger *slog.Logger) ([]*pb.FleetNodePairResult, bool) {
	if len(targets) == 0 {
		return nil, false
	}
	var (
		mu      sync.Mutex
		results []*pb.FleetNodePairResult
		wg      sync.WaitGroup
	)
	sem := make(chan struct{}, concurrency)
	for _, t := range targets {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			out, _ := waitSupervisor(&wg, &mu, &results, perPairTimeout*2, "pair", logger)
			return out, true
		}
		wg.Add(1)
		go func(target *pairingpb.FleetNodePairTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			pairCtx, cancel := context.WithTimeout(ctx, perPairTimeout)
			defer cancel()
			res := pair(pairCtx, target, creds)
			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}(t)
	}
	return waitSupervisor(&wg, &mu, &results, perPairTimeout*2, "pair", logger)
}

// truncateUTF8 trims s to at most maxLen bytes on a rune boundary so it stays valid
// UTF-8 and within the proto field cap.
func truncateUTF8(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	cut := maxLen - 3
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
