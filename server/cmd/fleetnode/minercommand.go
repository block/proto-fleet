package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	curtailmentpb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	errorspb "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	minercommandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/commandresult"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/passwordupdate"
	"github.com/block/proto-fleet/server/internal/domain/miner/logformat"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/sv2"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

var (
	// minerCommandTimeout bounds a short miner command. It must stay below the server's
	// WorkerExecutionTimeout (default 30s) minus ack slack, or a slow command can be
	// retried while the node still runs it (duplicate reboot/curtail). var so tests shrink it.
	minerCommandTimeout = 25 * time.Second
	// firmwareMinerCommandTimeout stays below the server-side firmware execution
	// budget (default 15m) so the node has time to ack before the worker expires.
	// Firmware updates include an artifact download and device install, so they should
	// not inherit the short command timeout used for reboot/curtailment commands.
	firmwareMinerCommandTimeout = 14 * time.Minute
)

const (
	supportedMiningPoolSlots       = 3
	maxSupportedMiningPoolPriority = supportedMiningPoolSlots - 1
	maxGetErrorsReports            = 512
	minerLogsArtifactFilename      = "miner-logs.csv"
	commandArtifactChunkSize       = 1 << 20
)

// driverGetter is the plugin-manager seam the executor needs; *plugins.Manager satisfies it.
type driverGetter interface {
	GetDriverByDriverName(driverName string) (sdk.Driver, error)
}

// secretProvider builds the auth bundle to reach a miner. Production decrypts the
// opaque descriptor credential with the node-local key; tests can inject an empty
// provider for no-secret drivers.
type secretProvider interface {
	SecretBundle(target *pb.MinerConnectionDescriptor) (sdk.SecretBundle, error)
	Seal(bundle sdk.SecretBundle) (*pb.EncryptedCredentials, error)
}

// nodeSecretProvider returns an empty bundle for tests and no-secret drivers.
type nodeSecretProvider struct{}

func (nodeSecretProvider) SecretBundle(_ *pb.MinerConnectionDescriptor) (sdk.SecretBundle, error) {
	return sdk.SecretBundle{}, nil
}

func (nodeSecretProvider) Seal(_ sdk.SecretBundle) (*pb.EncryptedCredentials, error) {
	return nil, cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "fleet node has no credential sealer configured")
}

func (r *RunCmd) handleMinerCommand(ctx context.Context, client gatewayClient, stream acker, commandID string, mc *pb.MinerCommand, logger *slog.Logger) {
	if r.driverGetter == nil || r.minerSecrets == nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "fleet node has no plugins loaded", logger)
		return
	}
	// handleCommand validated only the outer ControlCommand; validate the inner one here.
	if vErr := protovalidate.Validate(mc); vErr != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, fmt.Sprintf("invalid miner command: %v", vErr), logger)
		return
	}
	target := mc.GetTarget()
	if err := validateDialTarget(target); err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, err.Error(), logger)
		return
	}
	port, err := sdk.ParsePort(target.GetPort())
	if err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, fmt.Sprintf("invalid port: %v", err), logger)
		return
	}
	driver, err := r.driverGetter.GetDriverByDriverName(target.GetDriverName())
	if err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_AGENT_INCAPABLE, fmt.Sprintf("no plugin for driver %q: %v", target.GetDriverName(), err), logger)
		return
	}
	bundle, err := r.minerSecrets.SecretBundle(target)
	if err != nil {
		code, msg := classifyMinerCommandError("build secret bundle", err)
		r.sendAck(stream, commandID, code, msg, logger)
		return
	}
	passwordUpdate, err := newPasswordUpdateCommand(r.passwordUpdatePrivateKey, target, mc)
	if err != nil {
		code, msg := classifyMinerCommandError("decrypt password update", err)
		r.sendAck(stream, commandID, code, msg, logger)
		return
	}
	bundle = passwordUpdate.secretBundle(target, bundle)

	cmdCtx, cancel := context.WithTimeout(ctx, minerCommandActionTimeout(mc))
	defer cancel()

	result, err := driver.NewDevice(cmdCtx, target.GetDeviceIdentifier(), sdk.DeviceInfo{
		Host:         target.GetIpAddress(),
		Port:         port,
		URLScheme:    target.GetUrlScheme(),
		SerialNumber: target.GetSerialNumber(),
		MacAddress:   target.GetMacAddress(),
	}, bundle)
	if err != nil {
		code, msg := classifyMinerCommandError("connect to miner", err)
		r.sendAck(stream, commandID, code, msg, logger)
		return
	}
	dev := result.Device
	defer func() {
		// Best-effort release on a ctx that outlives a timed-out command.
		closeCtx, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer closeCancel()
		if cerr := dev.Close(closeCtx); cerr != nil {
			logger.Warn("closing device after command", "command_id", commandID, "err", cerr)
		}
	}()

	caps, err := commandCapabilities(cmdCtx, driver, mc)
	if err != nil {
		code, msg := classifyMinerCommandError("load driver capabilities", err)
		r.sendAck(stream, commandID, code, msg, logger)
		return
	}

	var payload []byte
	if passwordUpdate != nil {
		payload, err = passwordUpdate.run(cmdCtx, dev, bundle, r.minerSecrets)
	} else {
		payload, err = runMinerAction(cmdCtx, client, commandID, caps, dev, mc)
	}
	if err != nil {
		code, msg := classifyMinerActionError("execute command", mc, err)
		r.sendAck(stream, commandID, code, msg, logger)
		return
	}
	r.sendAckWithPayload(stream, commandID, pb.AckCode_ACK_CODE_OK, "", payload, logger)
}

type passwordUpdateCommand struct {
	secret passwordupdate.Secret
}

func newPasswordUpdateCommand(privateKey []byte, target *pb.MinerConnectionDescriptor, mc *pb.MinerCommand) (*passwordUpdateCommand, error) {
	update := mc.GetUpdateMinerPassword()
	if update == nil {
		return nil, nil
	}
	secret, err := decryptUpdateMinerPasswordSecret(privateKey, target, update)
	if err != nil {
		return nil, err
	}
	return &passwordUpdateCommand{secret: secret}, nil
}

func (u *passwordUpdateCommand) secretBundle(target *pb.MinerConnectionDescriptor, bundle sdk.SecretBundle) sdk.SecretBundle {
	if u == nil {
		return bundle
	}
	if current, ok := bundle.Kind.(sdk.UsernamePassword); ok {
		return sdk.SecretBundle{
			Version: bundle.Version,
			Kind: sdk.UsernamePassword{
				Username: current.Username,
				Password: u.secret.CurrentPassword,
			},
		}
	}
	if bundle.Kind != nil {
		return bundle
	}
	if target.GetDriverName() != minermodels.DriverNameProto {
		return bundle
	}
	return sdk.SecretBundle{
		Version: credentialPayloadVersion,
		Kind: sdk.UsernamePassword{
			Username: minermodels.ProtoDefaultUsername,
			Password: u.secret.CurrentPassword,
		},
	}
}

func (u *passwordUpdateCommand) run(ctx context.Context, dev sdk.Device, bundle sdk.SecretBundle, sealer secretProvider) ([]byte, error) {
	if u == nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "encrypted password update is required")
	}
	payload, err := updateMinerPasswordResultPayload(bundle, u.secret.NewPassword, sealer)
	if err != nil {
		return nil, err
	}
	if err := dev.UpdateMinerPassword(ctx, u.secret.CurrentPassword, u.secret.NewPassword); err != nil {
		return nil, err
	}
	return payload, nil
}

func minerCommandActionTimeout(mc *pb.MinerCommand) time.Duration {
	if mc.GetFirmwareUpdate() != nil {
		return firmwareMinerCommandTimeout
	}
	return minerCommandTimeout
}

// validateDialTarget rejects descriptors the node should never dial: a non-IP, public,
// or link-local address, or a scheme the drivers can't dial. Loopback is allowed for the
// dev virtual driver; mirrors the discovery path's private-address policy.
func validateDialTarget(t *pb.MinerConnectionDescriptor) error {
	addr, err := netip.ParseAddr(t.GetIpAddress())
	if err != nil {
		return fmt.Errorf("ip_address %q is not a valid IP", t.GetIpAddress())
	}
	if a := addr.Unmap(); !a.IsPrivate() && !a.IsLoopback() {
		return fmt.Errorf("ip_address %q is not a private or loopback address", t.GetIpAddress())
	}
	// Restrict to schemes the dial path accepts (networking.ProtocolFromString); rejects empty/unknown.
	if _, err := networking.ProtocolFromString(t.GetUrlScheme()); err != nil {
		return fmt.Errorf("unsupported url_scheme %q", t.GetUrlScheme())
	}
	return nil
}

func commandCapabilities(ctx context.Context, driver sdk.Driver, mc *pb.MinerCommand) (sdk.Capabilities, error) {
	if _, ok := mc.GetAction().(*pb.MinerCommand_DownloadLogs); !ok {
		return nil, nil
	}
	_, caps, err := driver.DescribeDriver(ctx)
	return caps, err
}

func runMinerAction(ctx context.Context, client gatewayClient, commandID string, caps sdk.Capabilities, dev sdk.Device, mc *pb.MinerCommand) ([]byte, error) {
	switch a := mc.GetAction().(type) {
	case *pb.MinerCommand_Reboot:
		return nil, dev.Reboot(ctx)
	case *pb.MinerCommand_StartMining:
		return nil, dev.StartMining(ctx)
	case *pb.MinerCommand_StopMining:
		return nil, dev.StopMining(ctx)
	case *pb.MinerCommand_BlinkLed:
		return nil, dev.BlinkLED(ctx)
	case *pb.MinerCommand_Curtail:
		level, err := toSDKCurtailLevel(a.Curtail.GetLevel())
		if err != nil {
			return nil, err
		}
		curtailer, ok := dev.(sdk.DeviceCurtailment)
		if !ok {
			return nil, sdk.NewErrUnsupportedCapability("curtailment")
		}
		return nil, curtailer.Curtail(ctx, sdk.CurtailRequest{Level: level})
	case *pb.MinerCommand_Uncurtail:
		curtailer, ok := dev.(sdk.DeviceCurtailment)
		if !ok {
			return nil, sdk.NewErrUnsupportedCapability("curtailment")
		}
		return nil, curtailer.Uncurtail(ctx, sdk.UncurtailRequest{})
	case *pb.MinerCommand_SetCoolingMode:
		mode, err := toSDKCoolingMode(a.SetCoolingMode.GetMode())
		if err != nil {
			return nil, err
		}
		return nil, dev.SetCoolingMode(ctx, mode)
	case *pb.MinerCommand_SetPowerTarget:
		mode, err := toSDKPerformanceMode(a.SetPowerTarget.GetPerformanceMode())
		if err != nil {
			return nil, err
		}
		return nil, dev.SetPowerTarget(ctx, mode)
	case *pb.MinerCommand_UpdateMiningPools:
		return nil, dev.UpdateMiningPools(ctx, toSDKMiningPoolConfigs(a.UpdateMiningPools.GetPools()))
	case *pb.MinerCommand_GetMiningPools:
		pools, err := dev.GetMiningPools(ctx)
		if err != nil {
			return nil, err
		}
		payload, err := getMiningPoolsResultPayload(pools)
		if err != nil {
			return nil, err
		}
		return payload, nil
	case *pb.MinerCommand_GetErrors:
		deviceErrors, err := dev.GetErrors(ctx)
		if err != nil {
			return nil, err
		}
		return getErrorsResultPayload(mc.GetTarget().GetDeviceIdentifier(), deviceErrors)
	case *pb.MinerCommand_DownloadLogs:
		logData, moreData, err := dev.DownloadLogs(ctx, nil, a.DownloadLogs.GetBatchLogUuid())
		if err != nil {
			return nil, err
		}
		payload, err := minerLogsArtifactPayload(logData, caps[sdk.CapabilityLogLevels])
		if err != nil {
			return nil, err
		}
		if _, err := uploadMinerLogsArtifact(ctx, client, commandID, mc.GetTarget().GetDeviceIdentifier(), payload); err != nil {
			return nil, err
		}
		if moreData {
			return nil, cmdErr(pb.AckCode_ACK_CODE_PARTIAL, "uploaded partial miner log data; retry after partial log pagination is supported")
		}
		return nil, nil
	case *pb.MinerCommand_FirmwareUpdate:
		return nil, runFirmwareUpdateAction(ctx, client, commandID, mc.GetTarget().GetDeviceIdentifier(), dev, a.FirmwareUpdate.GetArtifact())
	default:
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "unrecognized miner command action")
	}
}

func uploadMinerLogsArtifact(ctx context.Context, client gatewayClient, commandID string, deviceIdentifier string, payload []byte) (*pb.CommandArtifactRef, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client unavailable for miner log upload")
	}
	sum := sha256.Sum256(payload)
	sha := hex.EncodeToString(sum[:])

	stream := client.UploadCommandArtifact(ctx)
	if stream == nil {
		return nil, fmt.Errorf("gateway client returned nil command artifact upload stream")
	}
	if err := stream.Send(&pb.UploadCommandArtifactRequest{Part: &pb.UploadCommandArtifactRequest_Header{
		Header: &pb.CommandArtifactUploadHeader{
			CommandId:        commandID,
			Purpose:          pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_MINER_LOGS,
			Filename:         minerLogsArtifactFilename,
			SizeBytes:        int64(len(payload)),
			Sha256:           sha,
			DeviceIdentifier: deviceIdentifier,
		},
	}}); err != nil {
		return nil, fmt.Errorf("upload miner logs header: %w", err)
	}
	for offset := 0; offset < len(payload); offset += commandArtifactChunkSize {
		end := offset + commandArtifactChunkSize
		if end > len(payload) {
			end = len(payload)
		}
		if err := stream.Send(&pb.UploadCommandArtifactRequest{Part: &pb.UploadCommandArtifactRequest_Chunk{
			Chunk: &pb.CommandArtifactChunk{Data: payload[offset:end]},
		}}); err != nil {
			return nil, fmt.Errorf("upload miner logs chunk: %w", err)
		}
	}
	resp, err := stream.CloseAndReceive()
	if err != nil {
		return nil, fmt.Errorf("finish miner logs upload: %w", err)
	}
	if resp == nil || resp.Msg == nil || resp.Msg.GetArtifact() == nil {
		return nil, fmt.Errorf("miner logs upload returned no artifact")
	}
	ref := resp.Msg.GetArtifact()
	if ref.GetPurpose() != pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_MINER_LOGS {
		return nil, fmt.Errorf("miner logs upload returned artifact purpose %s", ref.GetPurpose())
	}
	if ref.GetSizeBytes() != int64(len(payload)) {
		return nil, fmt.Errorf("miner logs upload returned size %d, want %d", ref.GetSizeBytes(), len(payload))
	}
	if ref.GetSha256() != sha {
		return nil, fmt.Errorf("miner logs upload returned sha256 %q, want %q", ref.GetSha256(), sha)
	}
	return ref, nil
}

func minerLogsArtifactPayload(logData string, includeType bool) ([]byte, error) {
	if int64(len(logData)) > logformat.MaxArtifactBytes {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "miner log data exceeds %d byte download limit", logformat.MaxArtifactBytes)
	}
	payload := &limitedBuffer{limit: logformat.MaxArtifactBytes}
	if err := logformat.WriteTextToCSV(payload, logData, includeType); err != nil {
		return nil, err
	}
	return payload.Bytes(), nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit int64
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if int64(b.Len()+len(p)) > b.limit {
		return 0, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "miner log artifact exceeds %d byte download limit", b.limit)
	}
	n, _ := b.Buffer.Write(p)
	return n, nil
}

func decryptUpdateMinerPasswordSecret(privateKey []byte, target *pb.MinerConnectionDescriptor, action *pb.UpdateMinerPasswordAction) (passwordupdate.Secret, error) {
	if len(privateKey) == 0 {
		return passwordupdate.Secret{}, cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "fleet node has no password update decryption key configured")
	}
	secret, err := passwordupdate.Decrypt(privateKey, action.GetEncryptedPasswordUpdate(), target.GetDeviceIdentifier())
	if err != nil {
		return passwordupdate.Secret{}, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "%s", err.Error())
	}
	return secret, nil
}

func decodePasswordUpdatePrivateKey(st *bootstrap.State) ([]byte, error) {
	if st == nil || st.EncryptionPrivateKeyHex == "" {
		return nil, fmt.Errorf("state has no password update encryption private key; re-enroll the fleet node")
	}
	privateKey, err := hex.DecodeString(st.EncryptionPrivateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("decode password update encryption private key: %w", err)
	}
	if len(privateKey) != 32 {
		return nil, fmt.Errorf("password update encryption private key must be 32 bytes, got %d", len(privateKey))
	}
	return privateKey, nil
}

func updateMinerPasswordResultPayload(bundle sdk.SecretBundle, newPassword string, sealer secretProvider) ([]byte, error) {
	current, ok := bundle.Kind.(sdk.UsernamePassword)
	if !ok {
		return nil, cmdErr(pb.AckCode_ACK_CODE_UNAUTHENTICATED, "target credentials are required to update miner password")
	}
	encrypted, err := sealer.Seal(sdk.SecretBundle{
		Version: bundle.Version,
		Kind: sdk.UsernamePassword{
			Username: current.Username,
			Password: newPassword,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("seal updated miner password credentials: %w", err)
	}
	result := &pb.UpdateMinerPasswordResult{EncryptedCredentials: encrypted}
	if err := commandresult.ValidateUpdateMinerPassword(result); err != nil {
		return nil, err
	}
	payload, err := proto.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal update miner password result: %w", err)
	}
	return payload, nil
}

func runFirmwareUpdateAction(ctx context.Context, client gatewayClient, commandID, deviceIdentifier string, dev sdk.Device, ref *pb.CommandArtifactRef) error {
	if client == nil {
		return cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "fleet node gateway client is unavailable")
	}
	if ref == nil {
		return cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "firmware artifact is required")
	}
	if ref.GetPurpose() != pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_FIRMWARE_PAYLOAD {
		return cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "firmware artifact has wrong purpose: %s", ref.GetPurpose())
	}

	tmpPath, cleanup, err := downloadFirmwareArtifact(ctx, client, commandID, deviceIdentifier, ref)
	if err != nil {
		return err
	}
	defer cleanup()

	file, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("open downloaded firmware: %w", err)
	}
	defer file.Close()

	return dev.FirmwareUpdate(ctx, sdk.FirmwareFile{
		Reader:   file,
		ID:       ref.GetArtifactId(),
		Filename: ref.GetFilename(),
		Size:     ref.GetSizeBytes(),
		SHA256:   ref.GetSha256(),
		FilePath: tmpPath,
	})
}

func downloadFirmwareArtifact(ctx context.Context, client gatewayClient, commandID, deviceIdentifier string, ref *pb.CommandArtifactRef) (string, func(), error) {
	stream, err := client.DownloadCommandArtifact(ctx, connect.NewRequest(&pb.DownloadCommandArtifactRequest{
		CommandId:        commandID,
		Artifact:         ref,
		DeviceIdentifier: deviceIdentifier,
	}))
	if err != nil {
		return "", nil, fmt.Errorf("download firmware artifact: %w", err)
	}
	defer stream.Close()
	if !stream.Receive() {
		return "", nil, commandArtifactStreamErr(stream, "receive firmware artifact header")
	}
	if got := stream.Msg().GetHeader(); got == nil || !commandArtifactRefsEqual(got.GetArtifact(), ref) {
		return "", nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "firmware artifact header does not match command")
	}

	tmpDir, err := os.MkdirTemp("", "proto-fleet-firmware-*")
	if err != nil {
		return "", nil, fmt.Errorf("create firmware temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }
	filename := filepath.Base(ref.GetFilename())
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		filename = "firmware.bin"
	}
	tmpPath := filepath.Join(tmpDir, filename)
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("create firmware temp file: %w", err)
	}
	fail := func(err error) (string, func(), error) {
		_ = file.Close()
		cleanup()
		return "", nil, err
	}

	hasher := sha256.New()
	var written int64
	for stream.Receive() {
		chunk := stream.Msg().GetChunk()
		if chunk == nil {
			return fail(cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "firmware artifact stream contained non-chunk message after header"))
		}
		data := chunk.GetData()
		if len(data) == 0 {
			return fail(cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "firmware artifact chunk is empty"))
		}
		n, err := file.Write(data)
		if err != nil {
			return fail(fmt.Errorf("write firmware temp file: %w", err))
		}
		if n != len(data) {
			return fail(io.ErrShortWrite)
		}
		if _, err := hasher.Write(data); err != nil {
			return fail(fmt.Errorf("hash firmware chunk: %w", err))
		}
		written += int64(len(data))
		if written > ref.GetSizeBytes() {
			return fail(cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "firmware artifact is larger than declared"))
		}
	}
	if err := commandArtifactStreamErr(stream, "receive firmware artifact chunk"); err != nil && !errors.Is(err, io.EOF) {
		return fail(err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close firmware temp file: %w", err)
	}
	if written != ref.GetSizeBytes() {
		cleanup()
		return "", nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "firmware artifact size mismatch: declared %d bytes, received %d bytes", ref.GetSizeBytes(), written)
	}
	if actual := hex.EncodeToString(hasher.Sum(nil)); actual != ref.GetSha256() {
		cleanup()
		return "", nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "firmware artifact sha256 mismatch")
	}
	return tmpPath, cleanup, nil
}

func commandArtifactStreamErr(stream *connect.ServerStreamForClient[pb.DownloadCommandArtifactResponse], stage string) error {
	if err := stream.Err(); err != nil {
		return fmt.Errorf("%s: %w", stage, err)
	}
	return io.EOF
}

func commandArtifactRefsEqual(a, b *pb.CommandArtifactRef) bool {
	return a.GetArtifactId() == b.GetArtifactId() &&
		a.GetPurpose() == b.GetPurpose() &&
		a.GetFilename() == b.GetFilename() &&
		a.GetSizeBytes() == b.GetSizeBytes() &&
		a.GetSha256() == b.GetSha256()
}

func toSDKMiningPoolConfigs(pools []*pb.MiningPoolConfig) []sdk.MiningPoolConfig {
	sdkPools := make([]sdk.MiningPoolConfig, 0, len(pools))
	for _, pool := range pools {
		sdkPools = append(sdkPools, sdk.MiningPoolConfig{
			Priority:   pool.GetPriority(),
			URL:        pool.GetUrl(),
			WorkerName: pool.GetUsername(),
		})
	}
	return sdkPools
}

func getMiningPoolsResultPayload(pools []sdk.ConfiguredPool) ([]byte, error) {
	result := &pb.GetMiningPoolsResult{Pools: miningPoolConfigsFromSDK(pools)}
	if err := validateGetMiningPoolsResult(result); err != nil {
		return nil, err
	}
	payload, err := proto.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal get mining pools result: %w", err)
	}
	return payload, nil
}

func validateGetMiningPoolsResult(result *pb.GetMiningPoolsResult) error {
	if err := protovalidate.Validate(result); err != nil {
		return fmt.Errorf("invalid get mining pools result: %w", err)
	}
	for _, pool := range result.GetPools() {
		if err := sv2.ValidatePoolURL(pool.GetUrl()); err != nil {
			return fmt.Errorf("invalid get mining pools result: %w", err)
		}
	}
	return nil
}

func miningPoolConfigsFromSDK(pools []sdk.ConfiguredPool) []*pb.MiningPoolConfig {
	var defaultPool, backup1Pool, backup2Pool *pb.MiningPoolConfig
	for _, pool := range pools {
		config := &pb.MiningPoolConfig{
			Priority: pool.Priority,
			Url:      pool.URL,
			Username: pool.Username,
		}
		switch pool.Priority {
		case 0:
			if defaultPool == nil {
				defaultPool = config
			}
		case 1:
			if backup1Pool == nil {
				backup1Pool = config
			}
		case 2:
			if backup2Pool == nil {
				backup2Pool = config
			}
		default:
			if pool.Priority > maxSupportedMiningPoolPriority {
				continue
			}
			return []*pb.MiningPoolConfig{config}
		}
	}

	configured := make([]*pb.MiningPoolConfig, 0, supportedMiningPoolSlots)
	for _, pool := range []*pb.MiningPoolConfig{defaultPool, backup1Pool, backup2Pool} {
		if pool != nil {
			configured = append(configured, pool)
		}
	}
	return configured
}

func getErrorsResultPayload(targetDeviceID string, deviceErrors sdk.DeviceErrors) ([]byte, error) {
	result, err := getErrorsResultFromSDK(targetDeviceID, deviceErrors)
	if err != nil {
		return nil, err
	}
	if err := protovalidate.Validate(result); err != nil {
		return nil, fmt.Errorf("invalid get errors result: %w", err)
	}
	payload, err := proto.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal get errors result: %w", err)
	}
	if len(payload) <= maxAckPayloadBytes {
		return payload, nil
	}
	return truncateGetErrorsResultPayload(result)
}

func getErrorsResultFromSDK(targetDeviceID string, deviceErrors sdk.DeviceErrors) (*pb.GetErrorsResult, error) {
	pluginErrors := deviceErrors.Errors
	omittedReports := 0
	if len(pluginErrors) > maxGetErrorsReports {
		omittedReports = len(pluginErrors) - maxGetErrorsReports
		pluginErrors = pluginErrors[:maxGetErrorsReports]
	}
	result := &pb.GetErrorsResult{
		DeviceId:           targetDeviceID,
		Errors:             make([]*pb.MinerErrorReport, 0, len(pluginErrors)),
		Truncated:          omittedReports > 0,
		OmittedReportCount: uint32(omittedReports), // #nosec G115 -- omittedReports <= len(pluginErrors), bounded by memory.
	}
	for _, sdkErr := range pluginErrors {
		report := &pb.MinerErrorReport{
			MinerError:        errorspb.MinerError(sdkErr.MinerError),
			CauseSummary:      sdkErr.CauseSummary,
			RecommendedAction: sdkErr.RecommendedAction,
			Severity:          errorspb.Severity(sdkErr.Severity),
			VendorAttributes:  sdkErr.VendorAttributes,
			DeviceId:          targetDeviceID,
			ComponentId:       sdkErr.ComponentID,
			Impact:            sdkErr.Impact,
			Summary:           sdkErr.Summary,
			ComponentType:     errorspb.ComponentType(sdkErr.ComponentType),
		}
		if !sdkErr.FirstSeenAt.IsZero() {
			report.FirstSeenAt = timestamppb.New(sdkErr.FirstSeenAt)
		}
		if !sdkErr.LastSeenAt.IsZero() {
			report.LastSeenAt = timestamppb.New(sdkErr.LastSeenAt)
		}
		if sdkErr.ClosedAt != nil && !sdkErr.ClosedAt.IsZero() {
			report.ClosedAt = timestamppb.New(*sdkErr.ClosedAt)
		}
		result.Errors = append(result.Errors, report)
	}
	return result, nil
}

func truncateGetErrorsResultPayload(result *pb.GetErrorsResult) ([]byte, error) {
	originalCount := len(result.GetErrors()) + int(result.GetOmittedReportCount())
	low, high := 0, len(result.GetErrors())
	var best []byte
	for low <= high {
		mid := (low + high) / 2
		candidate := &pb.GetErrorsResult{
			DeviceId:           result.GetDeviceId(),
			Errors:             result.GetErrors()[:mid],
			Truncated:          mid < originalCount,
			OmittedReportCount: uint32(originalCount - mid), // #nosec G115 -- originalCount is bounded by the plugin response slice length.
		}
		payload, err := proto.Marshal(candidate)
		if err != nil {
			return nil, fmt.Errorf("marshal truncated get errors result: %w", err)
		}
		if len(payload) <= maxAckPayloadBytes {
			best = payload
			low = mid + 1
			continue
		}
		high = mid - 1
	}
	if best == nil {
		empty := &pb.GetErrorsResult{
			DeviceId:           result.GetDeviceId(),
			Truncated:          true,
			OmittedReportCount: uint32(originalCount), // #nosec G115 -- originalCount is bounded by the plugin response slice length.
		}
		payload, err := proto.Marshal(empty)
		if err != nil {
			return nil, fmt.Errorf("marshal empty get errors result: %w", err)
		}
		if len(payload) > maxAckPayloadBytes {
			return nil, cmdErr(pb.AckCode_ACK_CODE_PARTIAL, "get errors result metadata exceeds ack payload limit")
		}
		return payload, nil
	}
	return best, nil
}

// Reject undefined / non-actionable (UNSPECIFIED) enum values with BAD_REQUEST rather
// than casting them to a plugin. if/else (not switch) to sidestep the exhaustive linter.
func toSDKCoolingMode(m commonpb.CoolingMode) (sdk.CoolingMode, error) {
	if m == commonpb.CoolingMode_COOLING_MODE_AIR_COOLED {
		return sdk.CoolingModeAirCooled, nil
	}
	if m == commonpb.CoolingMode_COOLING_MODE_IMMERSION_COOLED {
		return sdk.CoolingModeImmersionCooled, nil
	}
	if m == commonpb.CoolingMode_COOLING_MODE_MANUAL {
		return sdk.CoolingModeManual, nil
	}
	return sdk.CoolingModeUnspecified, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "unsupported cooling mode: %s", m)
}

func toSDKPerformanceMode(m minercommandpb.PerformanceMode) (sdk.PerformanceMode, error) {
	if m == minercommandpb.PerformanceMode_PERFORMANCE_MODE_MAXIMUM_HASHRATE {
		return sdk.PerformanceModeMaximumHashrate, nil
	}
	if m == minercommandpb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY {
		return sdk.PerformanceModeEfficiency, nil
	}
	return sdk.PerformanceModeUnspecified, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "unsupported performance mode: %s", m)
}

func toSDKCurtailLevel(l curtailmentpb.CurtailmentLevel) (sdk.CurtailLevel, error) {
	if l == curtailmentpb.CurtailmentLevel_CURTAILMENT_LEVEL_EFFICIENCY {
		return sdk.CurtailLevelEfficiency, nil
	}
	if l == curtailmentpb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL {
		return sdk.CurtailLevelFull, nil
	}
	return sdk.CurtailLevelUnspecified, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "unsupported curtail level: %s", l)
}

// classifyMinerCommandError maps an SDK/plugin error to an ack code so the server reacts
// right (evict on auth, permanent-fail on unimplemented). if/else to dodge the exhaustive linter.
func classifyMinerCommandError(stage string, err error) (pb.AckCode, string) {
	msg := fmt.Sprintf("%s: %v", stage, err)
	// A typed command error (e.g. an undefined enum) carries its own ack code.
	var ce *commandError
	if errors.As(err, &ce) {
		return ce.code, msg
	}
	var sdkErr sdk.SDKError
	if errors.As(err, &sdkErr) {
		if sdkErr.Code == sdk.ErrCodeAuthenticationFailed {
			return pb.AckCode_ACK_CODE_UNAUTHENTICATED, msg
		}
		if sdkErr.Code == sdk.ErrCodeUnsupportedCapability || sdkErr.Code == sdk.ErrCodeCurtailCapabilityNotSupported {
			return pb.AckCode_ACK_CODE_UNIMPLEMENTED, msg
		}
	}
	if st, ok := grpcstatus.FromError(err); ok {
		if st.Code() == codes.Unauthenticated {
			return pb.AckCode_ACK_CODE_UNAUTHENTICATED, msg
		}
		if st.Code() == codes.Unimplemented {
			return pb.AckCode_ACK_CODE_UNIMPLEMENTED, msg
		}
	}
	return pb.AckCode_ACK_CODE_INTERNAL, msg
}

func classifyMinerActionError(stage string, mc *pb.MinerCommand, err error) (pb.AckCode, string) {
	code, msg := classifyMinerCommandError(stage, err)
	if code != pb.AckCode_ACK_CODE_INTERNAL || mc.GetUpdateMinerPassword() == nil {
		return code, msg
	}
	if st, ok := grpcstatus.FromError(err); ok && st.Code() == codes.FailedPrecondition {
		return pb.AckCode_ACK_CODE_UNAUTHENTICATED, msg
	}
	return code, msg
}
