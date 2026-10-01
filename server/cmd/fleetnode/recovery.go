package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"buf.build/go/protovalidate"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/domain/stableidentity"
	"github.com/block/proto-fleet/server/internal/infrastructure/cryptohash"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type recoveryEndpoint struct {
	ip, port, urlScheme, driverName string
	identity                        stableidentity.Identity
}

func (r *RunCmd) handleRecoverMinerEndpoints(ctx context.Context, stream acker, commandID string, req *pb.RecoverMinerEndpointsRequest, logger *slog.Logger) {
	if r.discoverer == nil || r.driverGetter == nil || r.minerSecrets == nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "fleet node has no plugins loaded", logger)
		return
	}
	if err := protovalidate.Validate(req); err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, fmt.Sprintf("invalid endpoint recovery request: %v", err), logger)
		return
	}

	cmdCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	results, partial, err := r.recoverMinerEndpoints(cmdCtx, req.GetTargets(), req.GetScanPorts(), logger)
	if err != nil && len(results) == 0 && !partial {
		code := pb.AckCode_ACK_CODE_SCAN_FAILED
		var ce *commandError
		if errors.As(err, &ce) {
			code = ce.code
		}
		r.sendAck(stream, commandID, code, err.Error(), logger)
		return
	}
	response := &pb.RecoverMinerEndpointsResult{Results: results}
	if validationErr := protovalidate.Validate(response); validationErr != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_INTERNAL, "endpoint recovery produced an invalid result", logger)
		return
	}
	// Keep a bounded prefix instead of letting the transport discard all results.
	for proto.Size(response) > maxAckPayloadBytes {
		response.Results = response.Results[:len(response.Results)-1]
		partial = true
	}
	payload, marshalErr := proto.Marshal(response)
	if marshalErr != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_INTERNAL, "marshal endpoint recovery result", logger)
		return
	}
	if partial || err != nil || cmdCtx.Err() != nil {
		r.sendAckWithPayload(stream, commandID, pb.AckCode_ACK_CODE_PARTIAL, "endpoint recovery completed partially", payload, logger)
		return
	}
	r.sendAckWithPayload(stream, commandID, pb.AckCode_ACK_CODE_OK, "", payload, logger)
}

func (r *RunCmd) recoverMinerEndpoints(ctx context.Context, targets []*pb.MinerConnectionDescriptor, scanPorts []string, logger *slog.Logger) ([]*pb.MinerEndpointRecoveryResult, bool, error) {
	endpoints, partial, scanErr := r.scanRecoveryEndpoints(ctx, scanPorts, logger)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, true, fmt.Errorf("endpoint recovery canceled: %w", ctxErr)
	}
	if partial {
		// An incomplete candidate set cannot safely prove FOUND, NOT_FOUND, or
		// AMBIGUOUS. Omit unresolved targets so the server leaves them untouched.
		return nil, true, scanErr
	}
	if scanErr != nil {
		return nil, false, scanErr
	}

	results := make([]*pb.MinerEndpointRecoveryResult, 0, len(targets))
	type inspection struct {
		endpoint recoveryEndpoint
		identity stableidentity.Identity
		err      error
	}
	inspectionCache := make(map[string]inspection)
	for _, target := range targets {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return results, true, fmt.Errorf("endpoint recovery canceled: %w", ctxErr)
		}
		want := stableidentity.New(target.GetSerialNumber(), target.GetMacAddress())
		if !want.Usable() {
			results = append(results, recoveryResult(target, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_UNRECOVERABLE_IDENTITY, recoveryEndpoint{}, ""))
			continue
		}
		if _, err := r.driverGetter.GetDriverByDriverName(target.GetDriverName()); err != nil {
			results = append(results, recoveryResult(target, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_ERROR, recoveryEndpoint{}, "target driver unavailable"))
			continue
		}
		bundle, err := r.minerSecrets.SecretBundle(target)
		if err != nil {
			results = append(results, recoveryResult(target, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_ERROR, recoveryEndpoint{}, "invalid encrypted credentials"))
			continue
		}

		credentialKey := recoveryCredentialKey(target.GetDriverName(), bundle)
		cacheKey := func(candidate recoveryEndpoint) string {
			return recoveryEndpointKey(candidate) + "\x00" + credentialKey
		}
		var candidates, pending []recoveryEndpoint
		for _, candidate := range endpoints {
			if candidate.driverName != target.GetDriverName() {
				continue
			}
			if candidate.urlScheme == "" {
				candidate.urlScheme = target.GetUrlScheme()
			}
			if want.Conflicts(candidate.identity) {
				continue
			}
			candidates = append(candidates, candidate)
			key := cacheKey(candidate)
			if _, cached := inspectionCache[key]; !cached {
				// Reserve the key before dispatch: deduplicate jobs and treat any
				// supervisor-truncated inspection as incomplete, never a match.
				inspectionCache[key] = inspection{err: errors.New("candidate inspection did not complete")}
				pending = append(pending, candidate)
			}
		}
		inspected, truncated := fanOutEndpointWork(ctx, slices.Values(pending), probeConcurrency, "recovery inspection", logger, func(probeCtx context.Context, candidate recoveryEndpoint) (inspection, bool) {
			if err := probeCtx.Err(); err != nil {
				return inspection{endpoint: candidate, err: err}, true
			}
			identity, err := r.inspectRecoveryEndpoint(probeCtx, target, candidate, bundle)
			if probeCtx.Err() != nil {
				err = probeCtx.Err()
			}
			return inspection{endpoint: candidate, identity: identity, err: err}, true
		})
		if ctxErr := ctx.Err(); ctxErr != nil {
			return results, true, fmt.Errorf("endpoint recovery canceled: %w", ctxErr)
		}
		if truncated {
			return results, true, nil
		}
		for _, result := range inspected {
			inspectionCache[cacheKey(result.endpoint)] = result
		}

		matches := make([]recoveryEndpoint, 0, 1)
		authFailedEndpoint := recoveryEndpoint{}
		identifiedEndpoints := make(map[string]struct{})
		inspectionFailed := false
		for _, candidate := range candidates {
			identifiedBeforeAuth := candidate.identity.Usable() && want.Matches(candidate.identity)
			if identifiedBeforeAuth {
				identifiedEndpoints[recoveryEndpointKey(candidate)] = struct{}{}
			}
			cached := inspectionCache[cacheKey(candidate)]
			got, err := cached.identity, cached.err
			if err != nil {
				if identifiedBeforeAuth && isAuthenticationError(err) {
					authFailedEndpoint = candidate
				} else if !isAuthenticationError(err) {
					inspectionFailed = true
				}
				continue
			}
			// Any shared identifier may establish a match, but conflicts reject
			// the candidate. With no shared identifier, leave it unmatched.
			if want.Matches(got) {
				candidate.identity = got
				identifiedEndpoints[recoveryEndpointKey(candidate)] = struct{}{}
				matches = append(matches, candidate)
			}
		}

		switch {
		case len(identifiedEndpoints) > 1:
			results = append(results, recoveryResult(target, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_AMBIGUOUS, recoveryEndpoint{}, ""))
		case inspectionFailed:
			results = append(results, recoveryResult(target, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_ERROR, recoveryEndpoint{}, "candidate could not be inspected"))
		case len(matches) == 1:
			results = append(results, recoveryResult(target, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND, matches[0], ""))
		case authFailedEndpoint.identity.Usable():
			results = append(results, recoveryResult(target, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_AUTHENTICATION_FAILED, authFailedEndpoint, ""))
		default:
			results = append(results, recoveryResult(target, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_NOT_FOUND, recoveryEndpoint{}, ""))
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return results, true, fmt.Errorf("endpoint recovery canceled: %w", ctxErr)
	}
	return results, partial, scanErr
}

func (r *RunCmd) scanRecoveryEndpoints(ctx context.Context, scanPorts []string, logger *slog.Logger) ([]recoveryEndpoint, bool, error) {
	ports := uniqueStrings(scanPorts)
	seenPorts := make(map[string]struct{}, len(ports))
	for _, port := range ports {
		seenPorts[port] = struct{}{}
	}
	for _, port := range r.discoverer.DefaultDiscoveryPorts(ctx) {
		if len(ports) == discoverylimits.MaxPortsPerIP {
			break
		}
		if _, seen := seenPorts[port]; seen {
			continue
		}
		seenPorts[port] = struct{}{}
		ports = append(ports, port)
	}
	parsedPorts, err := netscan.Ports(ports, nil)
	if err != nil {
		return nil, false, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "invalid recovery scan port: %s", err)
	}
	addrs, err := r.networkScanTargets(ctx, &pairingpb.NetworkScanModeRequest{Target: netscan.LocalSubnetTarget})
	if err != nil {
		return nil, false, err
	}
	r.scannerOnce.Do(func() {
		if r.scanner == nil {
			r.scanner = netscan.NewScanner()
		}
	})

	var endpoints []recoveryEndpoint
	scanErr := r.scanner.Scan(ctx, addrs, parsedPorts, func(host netscan.HostResult) error {
		for _, port := range host.OpenPorts {
			endpoints = append(endpoints, recoveryEndpoint{ip: host.Addr.String(), port: strconv.Itoa(int(port))})
		}
		return nil
	})
	var probeIncomplete atomic.Bool
	endpoints, probesTruncated := fanOutEndpointWork(ctx, slices.Values(endpoints), probeConcurrency, "recovery probe", logger, func(probeCtx context.Context, candidate recoveryEndpoint) (recoveryEndpoint, bool) {
		identity, scheme, driverName, err := r.probeRecoveryEndpoint(probeCtx, candidate.ip, candidate.port)
		if err != nil || driverName == "" {
			if err != nil && (probeCtx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || plugins.IsIncompleteDiscoveryError(err)) {
				probeIncomplete.Store(true)
			}
			return recoveryEndpoint{}, false
		}
		return recoveryEndpoint{ip: candidate.ip, port: candidate.port, urlScheme: scheme, driverName: driverName, identity: identity}, true
	})
	partial := scanErr != nil || probesTruncated || probeIncomplete.Load() || ctx.Err() != nil
	if ctxErr := ctx.Err(); ctxErr != nil {
		return endpoints, true, fmt.Errorf("recovery endpoint probing canceled: %w", ctxErr)
	}
	slices.SortFunc(endpoints, func(a, b recoveryEndpoint) int {
		if cmp := strings.Compare(a.ip, b.ip); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.port, b.port)
	})
	return endpoints, partial, scanErr
}

func uniqueStrings(values []string) []string {
	unique := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

type recoveryDiscoverer interface {
	ProbeRecovery(ctx context.Context, ipAddress, port string) (stableidentity.Identity, string, string, error)
}

type recoveryDeviceCleaner interface {
	CloseDevice(ctx context.Context, deviceID string) error
}

func (r *RunCmd) probeRecoveryEndpoint(ctx context.Context, ip, port string) (stableidentity.Identity, string, string, error) {
	if discoverer, ok := r.discoverer.(recoveryDiscoverer); ok {
		return discoverer.ProbeRecovery(ctx, ip, port)
	}
	report, err := r.discoverer.Probe(ctx, ip, port)
	if err != nil || report == nil {
		return stableidentity.Identity{}, "", "", err
	}
	return stableidentity.Identity{}, report.GetUrlScheme(), report.GetDriverName(), nil
}

func (r *RunCmd) inspectRecoveryEndpoint(ctx context.Context, target *pb.MinerConnectionDescriptor, endpoint recoveryEndpoint, bundle sdk.SecretBundle) (stableidentity.Identity, error) {
	driver, err := r.driverGetter.GetDriverByDriverName(target.GetDriverName())
	if err != nil {
		return stableidentity.Identity{}, err
	}
	port, err := sdk.ParsePort(endpoint.port)
	if err != nil {
		return stableidentity.Identity{}, err
	}
	scheme := endpoint.urlScheme
	if scheme == "" {
		scheme = target.GetUrlScheme()
	}
	deviceID := "endpoint-recovery-" + uuid.NewString()
	// Automatic recovery deliberately assumes a trusted LAN for this rollout.
	// Discovery MAC/serial values do not authenticate endpoints: a spoofed LAN
	// service can receive these saved credentials before identity is checked.
	result, err := driver.NewDevice(ctx, deviceID, sdk.DeviceInfo{
		Host: endpoint.ip, Port: port, URLScheme: scheme,
	}, bundle)
	if err != nil {
		code := grpcstatus.Code(err)
		uncertain := ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
			code == codes.Canceled || code == codes.DeadlineExceeded
		if cleaner, ok := driver.(recoveryDeviceCleaner); ok && uncertain {
			closeUncertainRecoveryDevice(ctx, cleaner, deviceID)
		}
		return stableidentity.Identity{}, err
	}
	if result.Device == nil {
		return stableidentity.Identity{}, errors.New("driver returned no device")
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = result.Device.Close(closeCtx)
	}()
	info, _, err := result.Device.DescribeDevice(ctx)
	if err != nil {
		return stableidentity.Identity{}, err
	}
	return stableidentity.New(info.SerialNumber, info.MacAddress), nil
}

func closeUncertainRecoveryDevice(ctx context.Context, cleaner recoveryDeviceCleaner, deviceID string) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	for {
		if cleaner.CloseDevice(closeCtx, deviceID) == nil {
			return
		}
		select {
		case <-closeCtx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func recoveryEndpointKey(endpoint recoveryEndpoint) string {
	return strings.Join([]string{endpoint.ip, endpoint.port, endpoint.urlScheme}, "\x00")
}

func recoveryCredentialKey(driver string, bundle sdk.SecretBundle) string {
	var material string
	if credentials, ok := bundle.Kind.(sdk.UsernamePassword); ok {
		material = credentials.Username + "\x00" + credentials.Password
	}
	return cryptohash.Sha256Hex(driver + "\x00" + material)
}

func isAuthenticationError(err error) bool {
	var sdkErr sdk.SDKError
	if errors.As(err, &sdkErr) && sdkErr.Code == sdk.ErrCodeAuthenticationFailed {
		return true
	}
	status, ok := grpcstatus.FromError(err)
	return ok && status.Code() == codes.Unauthenticated
}

func recoveryResult(target *pb.MinerConnectionDescriptor, outcome pb.MinerEndpointRecoveryOutcome, endpoint recoveryEndpoint, message string) *pb.MinerEndpointRecoveryResult {
	result := &pb.MinerEndpointRecoveryResult{
		DeviceIdentifier: target.GetDeviceIdentifier(),
		Outcome:          outcome,
		IpAddress:        endpoint.ip,
		Port:             endpoint.port,
		UrlScheme:        endpoint.urlScheme,
		SerialNumber:     endpoint.identity.SerialNumber,
		MacAddress:       endpoint.identity.MACAddress,
		ErrorMessage:     message,
	}
	if err := protovalidate.Validate(result); err == nil {
		return result
	}
	return &pb.MinerEndpointRecoveryResult{
		DeviceIdentifier: target.GetDeviceIdentifier(),
		Outcome:          pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_ERROR,
		ErrorMessage:     "plugin returned invalid recovery evidence",
	}
}
