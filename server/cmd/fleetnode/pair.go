package main

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const pairConcurrency = 16

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
	// minerSigningPubKey is the SPKI-DER base64 form of the node's miner-signing
	// public key, matching token.Service.ExtractPublicKeyFromPrivateKey so a
	// miner paired here trusts the JWTs the node signs at runtime.
	minerSigningPubKey string
}

func newPluginPairer(manager *plugins.Manager, minerSigningPrivKeyHex string) (*pluginPairer, error) {
	pub, err := minerSigningPublicKeySPKIBase64(minerSigningPrivKeyHex)
	if err != nil {
		return nil, err
	}
	return &pluginPairer{manager: manager, minerSigningPubKey: pub}, nil
}

func (p *pluginPairer) Pair(ctx context.Context, target *pairingpb.FleetNodePairTarget, creds *pairingpb.Credentials) *pb.FleetNodePairResult {
	res := &pb.FleetNodePairResult{DeviceIdentifier: target.GetDeviceIdentifier()}

	plugin, err := p.manager.GetPluginByDriverNameWithCapability(target.GetDriverName(), sdk.CapabilityPairing)
	if err != nil {
		res.Outcome = pb.PairOutcome_PAIR_OUTCOME_ERROR
		res.ErrorMessage = truncateUTF8(fmt.Sprintf("no pairing-capable driver %q: %v", target.GetDriverName(), err), maxAckErrorMessageBytes)
		return res
	}

	port, _ := sdk.ParsePort(target.GetPort())
	deviceInfo := sdk.DeviceInfo{
		Host:            target.GetIpAddress(),
		Port:            port,
		URLScheme:       target.GetUrlScheme(),
		Manufacturer:    target.GetManufacturer(),
		FirmwareVersion: target.GetFirmwareVersion(),
	}

	// Asymmetric-auth drivers (Proto) pair with the node's own miner-signing key;
	// operator-supplied username/password covers basic-auth drivers.
	if bundle, ok := secretBundleFor(plugin.Caps, p.minerSigningPubKey, creds); ok {
		return p.attempt(ctx, plugin, deviceInfo, bundle, res)
	}

	// No credentials supplied: try plugin-provided defaults, if any.
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

// secretBundleFor returns the auth bundle for a driver: the node's miner-signing
// key for asymmetric-auth drivers, supplied username/password otherwise. ok is
// false when no credentials apply (the caller falls back to plugin defaults or
// reports AUTH_NEEDED).
func secretBundleFor(caps sdk.Capabilities, nodePubKey string, creds *pairingpb.Credentials) (sdk.SecretBundle, bool) {
	if caps[sdk.CapabilityAsymmetricAuth] {
		return sdk.SecretBundle{Version: "v1", Kind: sdk.APIKey{Key: nodePubKey}}, true
	}
	if creds != nil && creds.Password != nil {
		return sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Username: creds.GetUsername(), Password: creds.GetPassword()}}, true
	}
	return sdk.SecretBundle{}, false
}

func (p *pluginPairer) attempt(ctx context.Context, plugin *plugins.LoadedPlugin, deviceInfo sdk.DeviceInfo, bundle sdk.SecretBundle, res *pb.FleetNodePairResult) *pb.FleetNodePairResult {
	updated, err := plugin.Driver.PairDevice(ctx, deviceInfo, bundle)
	if err != nil {
		classifyNodePairError(err, res)
		return res
	}
	setPaired(res, updated)
	return res
}

func setPaired(res *pb.FleetNodePairResult, info sdk.DeviceInfo) {
	res.Outcome = pb.PairOutcome_PAIR_OUTCOME_PAIRED
	res.SerialNumber = info.SerialNumber
	res.MacAddress = info.MacAddress
	res.Model = info.Model
	res.Manufacturer = info.Manufacturer
	res.FirmwareVersion = info.FirmwareVersion
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

// minerSigningPublicKeySPKIBase64 derives the SPKI-DER base64 public key from a
// hex-encoded ed25519 private key. It must produce the identical string to
// token.Service.ExtractPublicKeyFromPrivateKey (pinned by a cross-check test) so
// miners paired here trust the node's runtime JWTs.
func minerSigningPublicKeySPKIBase64(privKeyHex string) (string, error) {
	raw, err := hex.DecodeString(privKeyHex)
	if err != nil {
		return "", fmt.Errorf("decode miner signing private key: %w", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("miner signing private key is %d bytes, want %d", len(raw), ed25519.PrivateKeySize)
	}
	priv := ed25519.PrivateKey(raw)
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return "", fmt.Errorf("miner signing key is not ed25519")
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("marshal miner signing public key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(der), nil
}

// handlePairCommand pairs a batch of discovered devices and streams the results
// back. It mirrors the discovery path: bounded fan-out, chunked report upload,
// PARTIAL on deadline. Per-device outcomes ride the report, not the ack.
func (r *RunCmd) handlePairCommand(ctx context.Context, client gatewayClient, stream acker, commandID string, req *pairingpb.FleetNodePairRequest, logger *slog.Logger) {
	if r.pairer == nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "pairing unavailable: no plugins loaded", logger)
		return
	}
	targets := req.GetTargets()
	if len(targets) == 0 {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, "pair request has no targets", logger)
		return
	}
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
			out, _ := waitPairSupervisor(&wg, &mu, &results, perPairTimeout*2, logger)
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
	return waitPairSupervisor(&wg, &mu, &results, perPairTimeout*2, logger)
}

// waitPairSupervisor caps wg.Wait at maxWait so a plugin PairDevice that ignores
// ctx can't pin the agent; truncated=true lets the caller ack PARTIAL.
func waitPairSupervisor(wg *sync.WaitGroup, mu *sync.Mutex, results *[]*pb.FleetNodePairResult, maxWait time.Duration, logger *slog.Logger) ([]*pb.FleetNodePairResult, bool) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	truncated := false
	select {
	case <-done:
	case <-timer.C:
		truncated = true
		logger.Warn("pair wait exceeded supervisor budget; returning partial batch", "max_wait", maxWait.String())
	}
	mu.Lock()
	defer mu.Unlock()
	out := make([]*pb.FleetNodePairResult, len(*results))
	copy(out, *results)
	return out, truncated
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
