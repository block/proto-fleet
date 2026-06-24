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
	maxUsedPasswordBytes = 1024
)

// credentialsReportable reports whether username/password fit inside the
// encrypted credential report caps with conservative room for sealing overhead.
// We refuse an oversized credential rather than pair with it: the node could
// authenticate but the cloud couldn't persist it back, leaving the device PAIRED
// but unusable.
func credentialsReportable(username, password string) bool {
	return len(username) <= maxPairIdentityBytes && len(password) <= maxUsedPasswordBytes
}

// perPairTimeout bounds one device's auth handshake. var so tests can shrink it.
var perPairTimeout = 60 * time.Second

// pairer authenticates one discovered device and returns its per-device result.
// It never returns an error: auth and plugin failures map to a PairOutcome so a
// single bad device never fails the batch.
type pairer interface {
	Pair(ctx context.Context, target *pairingpb.FleetNodePairTarget, creds *pairingpb.Credentials) *pb.FleetNodePairResult
}

type credentialSealer interface {
	Seal(bundle sdk.SecretBundle) (*pb.EncryptedCredentials, error)
}

type pluginPairer struct {
	manager     *plugins.Manager
	credentials credentialSealer
}

func newPluginPairer(manager *plugins.Manager, credentials credentialSealer) *pluginPairer {
	return &pluginPairer{manager: manager, credentials: credentials}
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

	if bundle, ok := secretBundleFor(plugin.Caps, creds); ok {
		if !credentialsReportable(creds.GetUsername(), creds.GetPassword()) {
			res.Outcome = pb.PairOutcome_PAIR_OUTCOME_ERROR
			res.ErrorMessage = "supplied credentials exceed the maximum reportable size"
			return res
		}
		encrypted, err := p.credentialReport(bundle)
		if err != nil {
			res.Outcome = pb.PairOutcome_PAIR_OUTCOME_ERROR
			res.ErrorMessage = truncateUTF8(fmt.Sprintf("encrypt credentials: %v", err), maxAckErrorMessageBytes)
			return res
		}
		updated, pairErr := plugin.Driver.PairDevice(ctx, deviceInfo, bundle)
		if pairErr != nil {
			classifyNodePairError(pairErr, res)
			return res
		}
		setPaired(res, updated)
		res.EncryptedCredentials = encrypted
		return res
	}

	if provider, ok := plugin.Driver.(sdk.DefaultCredentialsProvider); ok {
		defaults := provider.GetDefaultCredentials(ctx, target.GetManufacturer(), target.GetFirmwareVersion())
		for _, c := range defaults {
			if !credentialsReportable(c.Username, c.Password) {
				continue
			}
			bundle := sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Username: c.Username, Password: c.Password}}
			encrypted, err := p.credentialReport(bundle)
			if err != nil {
				res.Outcome = pb.PairOutcome_PAIR_OUTCOME_ERROR
				res.ErrorMessage = truncateUTF8(fmt.Sprintf("encrypt credentials: %v", err), maxAckErrorMessageBytes)
				return res
			}
			updated, pairErr := plugin.Driver.PairDevice(ctx, deviceInfo, bundle)
			if pairErr != nil {
				if isNodeAuthFailure(pairErr) {
					continue
				}
				classifyNodePairError(pairErr, res)
				return res
			}
			setPaired(res, updated)
			res.EncryptedCredentials = encrypted
			return res
		}
	}

	res.Outcome = pb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED
	res.ErrorMessage = "credentials required for pairing"
	return res
}

func (p *pluginPairer) credentialReport(bundle sdk.SecretBundle) (*pb.EncryptedCredentials, error) {
	if p.credentials == nil {
		if bundle.Kind == nil {
			return nil, nil
		}
		return nil, errors.New("credential sealer is not configured")
	}
	encrypted, err := p.credentials.Seal(bundle)
	if err != nil {
		return nil, err
	}
	return encrypted, nil
}

// secretBundleFor returns the supplied username/password bundle. ok is false
// when no credentials apply (the caller falls back to plugin defaults or reports
// AUTH_NEEDED).
func secretBundleFor(_ sdk.Capabilities, creds *pairingpb.Credentials) (sdk.SecretBundle, bool) {
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
	res.DefaultPasswordActive = info.DefaultPasswordActive
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

	// One pair command at a time, held until every worker has exited: a truncated
	// batch abandons ctx-ignoring workers that may still be mutating miners, and a
	// second command must not race them. BUSY maps to a retryable operator error.
	if !r.pairMu.TryLock() {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BUSY, "a pair command is still running on this node; retry shortly", logger)
		return
	}

	cmdCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	results, truncated, workersDone := fanOutPairs(cmdCtx, targets, req.GetCredentials(), pairConcurrency, r.pairer.Pair, logger)
	go func() {
		<-workersDone
		r.pairMu.Unlock()
	}()

	// Stream on the parent ctx, not cmdCtx: a deadline-hit cmdCtx must not
	// suppress upload of the results already collected.
	rejected, err := r.streamPairResults(ctx, client, commandID, results, logger)
	if err != nil {
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
	// RejectedCount > 0 means the cloud didn't store a miner the node paired, so ack
	// PARTIAL (not OK) and let the operator re-list and re-issue the remainder.
	if rejected > 0 {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_PARTIAL, fmt.Sprintf("cloud did not persist %d of %d reported result(s); re-list and retry", rejected, len(results)), logger)
		return
	}
	r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_OK, "", logger)
}

// streamPairResults uploads results in chunks and returns how many the gateway
// failed to persist, so the caller can ack PARTIAL instead of claiming full success.
func (r *RunCmd) streamPairResults(ctx context.Context, client gatewayClient, commandID string, results []*pb.FleetNodePairResult, logger *slog.Logger) (int64, error) {
	var rejected int64
	for chunk := range slices.Chunk(results, maxDevicesPerReport) {
		callCtx, cancel := context.WithTimeout(ctx, discoveryReportTimeout)
		resp, err := client.ReportPairedDevices(callCtx, connect.NewRequest(&pb.ReportPairedDevicesRequest{
			CommandId: commandID,
			Results:   chunk,
		}))
		cancel()
		if err != nil {
			logger.Error("pair report failed", "command_id", commandID, "err", err)
			return rejected, fmt.Errorf("report paired devices: %w", err)
		}
		rejected += resp.Msg.GetRejectedCount()
		logger.Info("pair report accepted", "command_id", commandID, "batch_size", len(chunk), "rejected", resp.Msg.GetRejectedCount())
	}
	return rejected, nil
}

// fanOutPairs pairs targets with bounded concurrency, returning collected
// results, whether the batch was truncated (a hung plugin or a cancelled parent
// ctx left some targets unattempted; the operator re-lists and retries), and a
// channel closed once every started worker has exited. A truncated batch abandons
// ctx-ignoring workers that may still be mutating miners; the caller must not
// admit another pair command until that channel closes.
func fanOutPairs(ctx context.Context, targets []*pairingpb.FleetNodePairTarget, creds *pairingpb.Credentials, concurrency int, pair func(context.Context, *pairingpb.FleetNodePairTarget, *pairingpb.Credentials) *pb.FleetNodePairResult, logger *slog.Logger) ([]*pb.FleetNodePairResult, bool, <-chan struct{}) {
	var (
		mu      sync.Mutex
		results []*pb.FleetNodePairResult
		wg      sync.WaitGroup
	)
	// Called only after the spawn loop stops, so a transient wg zero-crossing
	// mid-spawn can't close the channel while workers are still being added.
	workersDone := func() <-chan struct{} {
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		return done
	}
	if len(targets) == 0 {
		return nil, false, workersDone()
	}
	sem := make(chan struct{}, concurrency)
	for _, t := range targets {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			out, _ := waitSupervisor(&wg, &mu, &results, perPairTimeout*2, "pair", logger)
			return out, true, workersDone()
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
	out, truncated := waitSupervisor(&wg, &mu, &results, perPairTimeout*2, "pair", logger)
	return out, truncated, workersDone()
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
