package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

const (
	commandArtifactChunkSize = 1 << 20
	// CommandArtifactUploadReadMaxBytes caps each protobuf message before
	// Connect unmarshals it. It is slightly larger than the logical chunk limit
	// to allow protobuf framing and small header messages.
	CommandArtifactUploadReadMaxBytes = commandArtifactChunkSize + 4096
)

var (
	// CommandArtifactUploadHeaderTimeout bounds the wait for the first upload
	// message, so a node can't reserve one of its upload slots and never send a
	// header. Vars so tests can shrink them.
	CommandArtifactUploadHeaderTimeout = 5 * time.Second
	CommandArtifactUploadChunkTimeout  = 30 * time.Second
	CommandArtifactUploadTotalTimeout  = 10 * time.Minute

	CommandArtifactDownloadTotalTimeout = 10 * time.Minute
)

type Handler struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler

	enrollment *enrollment.Service
	auth       *auth.Service
	pairing    *pairing.Service
	registry   *control.Registry
	files      *files.Service
}

var _ fleetnodegatewayv1connect.FleetNodeGatewayServiceHandler = &Handler{}

func NewHandler(enrollment *enrollment.Service, auth *auth.Service, pairing *pairing.Service, registry *control.Registry, filesService *files.Service) *Handler {
	return &Handler{enrollment: enrollment, auth: auth, pairing: pairing, registry: registry, files: filesService}
}

func CommandArtifactUploadReadLimitOption() connect.HandlerOption {
	return connect.WithConditionalHandlerOptions(func(spec connect.Spec) []connect.HandlerOption {
		if spec.Procedure != fleetnodegatewayv1connect.FleetNodeGatewayServiceUploadCommandArtifactProcedure {
			return nil
		}
		return []connect.HandlerOption{connect.WithReadMaxBytes(CommandArtifactUploadReadMaxBytes)}
	})
}

func (h *Handler) Register(ctx context.Context, req *connect.Request[pb.RegisterRequest]) (*connect.Response[pb.RegisterResponse], error) {
	agent, _, err := h.enrollment.RegisterFleetNode(ctx, req.Msg.GetEnrollmentToken(), req.Msg.GetName(), req.Msg.GetIdentityPubkey(), req.Msg.GetEncryptionPubkey())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RegisterResponse{
		FleetNodeId:         agent.ID,
		EnrollmentStatus:    pb.EnrollmentStatus_ENROLLMENT_STATUS_PENDING,
		IdentityFingerprint: enrollment.IdentityFingerprint(agent.IdentityPubkey),
	}), nil
}

func (h *Handler) BeginAuthHandshake(ctx context.Context, req *connect.Request[pb.BeginAuthHandshakeRequest]) (*connect.Response[pb.BeginAuthHandshakeResponse], error) {
	challenge, expiresAt, err := h.auth.BeginHandshake(ctx, req.Msg.GetApiKey(), req.Msg.GetIdentityPubkey())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.BeginAuthHandshakeResponse{
		Challenge: challenge,
		ExpiresAt: timestamppb.New(expiresAt),
	}), nil
}

func (h *Handler) CompleteAuthHandshake(ctx context.Context, req *connect.Request[pb.CompleteAuthHandshakeRequest]) (*connect.Response[pb.CompleteAuthHandshakeResponse], error) {
	token, expiresAt, err := h.auth.CompleteHandshake(ctx, req.Msg.GetChallenge(), req.Msg.GetSignature())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CompleteAuthHandshakeResponse{
		SessionToken: token,
		ExpiresAt:    timestamppb.New(expiresAt),
	}), nil
}

func (h *Handler) UploadHeartbeat(ctx context.Context, _ *connect.Request[pb.UploadHeartbeatRequest]) (*connect.Response[pb.UploadHeartbeatResponse], error) {
	subject, err := auth.GetSubject(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err := h.enrollment.UpdateLastSeen(ctx, subject.FleetNodeID, subject.OrgID, now); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UploadHeartbeatResponse{
		ReceivedAt: timestamppb.New(now),
	}), nil
}

func (h *Handler) UploadCommandArtifact(ctx context.Context, stream *connect.ClientStream[pb.UploadCommandArtifactRequest]) (*connect.Response[pb.UploadCommandArtifactResponse], error) {
	subject, err := auth.GetSubject(ctx)
	if err != nil {
		return nil, err
	}
	if h.files == nil {
		return nil, fleeterror.NewInternalError("command artifact store is not configured")
	}
	releaseUpload, err := h.registry.AcquireCommandArtifactUpload(subject.FleetNodeID)
	if err != nil {
		return nil, mapArtifactAdmissionError(err)
	}
	defer releaseUpload()

	uploadReceiver := newCommandArtifactUploadReceiver(stream)
	defer uploadReceiver.Close()

	msg, err := uploadReceiver.Receive(ctx, CommandArtifactUploadHeaderTimeout, "command artifact upload header")
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fleeterror.NewInvalidArgumentError("first UploadCommandArtifactRequest must be header")
		}
		return nil, err
	}
	header := msg.GetHeader()
	if header == nil {
		return nil, fleeterror.NewInvalidArgumentError("first UploadCommandArtifactRequest must be header")
	}
	expectation := control.ArtifactExpectation{
		Direction:        control.ArtifactDirectionUpload,
		Purpose:          header.GetPurpose(),
		DeviceIdentifier: header.GetDeviceIdentifier(),
	}
	commandDone, err := h.registry.AdmitCommandArtifactTransfer(subject.FleetNodeID, header.GetCommandId(), expectation)
	if err != nil {
		if errors.Is(err, control.ErrArtifactAlreadyTransferred) {
			if ref, ok := h.registry.CompletedCommandArtifactUpload(subject.FleetNodeID, header.GetCommandId(), expectation); ok && commandArtifactUploadHeaderMatchesRef(header, ref) {
				commandDone, err := h.registry.AdmitCompletedCommandArtifactUploadRetry(subject.FleetNodeID, header.GetCommandId(), expectation)
				if err != nil {
					return nil, mapArtifactAdmissionError(err)
				}
				defer h.registry.FinishCompletedCommandArtifactUploadRetry(subject.FleetNodeID, header.GetCommandId(), expectation)
				uploadCtx, cancel := commandArtifactTransferContext(ctx, commandDone, CommandArtifactUploadTotalTimeout)
				defer cancel()
				if err := drainCommandArtifactUploadRetry(uploadCtx, uploadReceiver, ref); err != nil {
					return nil, err
				}
				return connect.NewResponse(&pb.UploadCommandArtifactResponse{Artifact: ref}), nil
			}
		}
		return nil, mapArtifactAdmissionError(err)
	}

	uploadCtx, cancel := commandArtifactTransferContext(ctx, commandDone, CommandArtifactUploadTotalTimeout)
	defer cancel()
	artifact, err := h.files.SaveCommandArtifact(
		header.GetFilename(),
		header.GetSizeBytes(),
		header.GetSha256(),
		&commandArtifactUploadReader{receive: func() (*pb.UploadCommandArtifactRequest, error) {
			return uploadReceiver.Receive(uploadCtx, CommandArtifactUploadChunkTimeout, "command artifact upload chunk")
		}},
	)
	if err != nil {
		h.registry.ReinstateCommandArtifactUpload(subject.FleetNodeID, header.GetCommandId(), expectation)
		return nil, err
	}
	ref := commandArtifactRef(artifact, header.GetPurpose())
	if !h.registry.CompleteCommandArtifactUpload(subject.FleetNodeID, header.GetCommandId(), expectation, ref) {
		if err := h.files.DeleteCommandArtifact(ref.GetArtifactId()); err != nil {
			slog.Warn("failed to delete command artifact after command ended before upload completion", "artifact_id", ref.GetArtifactId(), "error", err)
		}
		return nil, fleeterror.NewFailedPreconditionError("command no longer in flight for artifact upload")
	}
	return connect.NewResponse(&pb.UploadCommandArtifactResponse{
		Artifact: ref,
	}), nil
}

func (h *Handler) DownloadCommandArtifact(ctx context.Context, req *connect.Request[pb.DownloadCommandArtifactRequest], stream *connect.ServerStream[pb.DownloadCommandArtifactResponse]) error {
	subject, err := auth.GetSubject(ctx)
	if err != nil {
		return err
	}
	if h.files == nil {
		return fleeterror.NewInternalError("command artifact store is not configured")
	}
	ref := req.Msg.GetArtifact()
	if ref == nil {
		return fleeterror.NewInvalidArgumentError("artifact is required")
	}
	releaseDownload, err := h.registry.AcquireCommandArtifactDownload(subject.FleetNodeID)
	if err != nil {
		return mapArtifactAdmissionError(err)
	}
	defer releaseDownload()

	expectation := control.ArtifactExpectation{
		Direction:        control.ArtifactDirectionDownload,
		Purpose:          ref.GetPurpose(),
		ArtifactID:       ref.GetArtifactId(),
		DeviceIdentifier: req.Msg.GetDeviceIdentifier(),
	}
	commandDone, err := h.registry.AdmitCommandArtifactTransfer(subject.FleetNodeID, req.Msg.GetCommandId(), expectation)
	if err != nil {
		return mapArtifactAdmissionError(err)
	}
	defer h.registry.ReinstateCommandArtifactTransfer(subject.FleetNodeID, req.Msg.GetCommandId(), expectation)

	reader, info, err := h.files.OpenCommandArtifact(ref.GetArtifactId())
	if err != nil {
		return err
	}
	defer reader.Close()
	if info.Size != ref.GetSizeBytes() || info.SHA256 != ref.GetSha256() {
		return fleeterror.NewFailedPreconditionError("command artifact metadata no longer matches the issued reference")
	}

	downloadCtx, cancel := commandArtifactTransferContext(ctx, commandDone, CommandArtifactDownloadTotalTimeout)
	defer cancel()

	if err := sendCommandArtifactDownloadResponse(downloadCtx, stream, &pb.DownloadCommandArtifactResponse{Part: &pb.DownloadCommandArtifactResponse_Header{
		Header: &pb.CommandArtifactDownloadHeader{Artifact: commandArtifactRef(&info, ref.GetPurpose())},
	}}); err != nil {
		return commandArtifactDownloadSendError("send command artifact header", err)
	}

	buf := make([]byte, commandArtifactChunkSize)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if err := sendCommandArtifactDownloadResponse(downloadCtx, stream, &pb.DownloadCommandArtifactResponse{Part: &pb.DownloadCommandArtifactResponse_Chunk{
				Chunk: &pb.CommandArtifactChunk{Data: chunk},
			}}); err != nil {
				return commandArtifactDownloadSendError("send command artifact chunk", err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return fleeterror.NewInternalErrorf("read command artifact: %v", readErr)
		}
	}
}

func sendCommandArtifactDownloadResponse(ctx context.Context, stream *connect.ServerStream[pb.DownloadCommandArtifactResponse], msg *pb.DownloadCommandArtifactResponse) error {
	if err := ctx.Err(); err != nil {
		return contextConnectError(ctx.Err(), "command artifact download deadline exceeded")
	}
	if err := stream.Send(msg); err != nil {
		return fmt.Errorf("send command artifact download response: %w", err)
	}
	return nil
}

func commandArtifactTransferContext(parent context.Context, commandDone <-chan struct{}, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	go func() {
		select {
		case <-commandDone:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func commandArtifactDownloadSendError(label string, err error) error {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) && connectErr.Code() == connect.CodeDeadlineExceeded {
		return err
	}
	return fleeterror.NewInternalErrorf("%s: %v", label, err)
}

func commandArtifactUploadHeaderMatchesRef(header *pb.CommandArtifactUploadHeader, ref *pb.CommandArtifactRef) bool {
	return header.GetPurpose() == ref.GetPurpose() &&
		header.GetSizeBytes() == ref.GetSizeBytes() &&
		header.GetSha256() == ref.GetSha256()
}

func drainCommandArtifactUploadRetry(ctx context.Context, uploadReceiver *commandArtifactUploadReceiver, ref *pb.CommandArtifactRef) error {
	hasher := sha256.New()
	reader := &commandArtifactUploadReader{receive: func() (*pb.UploadCommandArtifactRequest, error) {
		return uploadReceiver.Receive(ctx, CommandArtifactUploadChunkTimeout, "command artifact upload retry chunk")
	}}
	written, err := io.Copy(io.Discard, io.TeeReader(io.LimitReader(reader, ref.GetSizeBytes()+1), hasher))
	if err != nil {
		var fleetErr fleeterror.FleetError
		if errors.As(err, &fleetErr) {
			return fleetErr
		}
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return connectErr
		}
		return fmt.Errorf("drain command artifact retry: %w", err)
	}
	if written > ref.GetSizeBytes() {
		return fleeterror.NewInvalidArgumentErrorf("command artifact retry size mismatch: stored %d bytes, received more", ref.GetSizeBytes())
	}
	if written != ref.GetSizeBytes() {
		return fleeterror.NewInvalidArgumentErrorf("command artifact retry size mismatch: stored %d bytes, received %d bytes", ref.GetSizeBytes(), written)
	}
	if hex.EncodeToString(hasher.Sum(nil)) != ref.GetSha256() {
		return fleeterror.NewInvalidArgumentError("command artifact retry sha256 mismatch")
	}
	return nil
}

type commandArtifactUploadReader struct {
	receive func() (*pb.UploadCommandArtifactRequest, error)
	buf     []byte
}

func (r *commandArtifactUploadReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		msg, err := r.receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0, io.EOF
			}
			return 0, err
		}
		chunk := msg.GetChunk()
		if chunk == nil {
			return 0, fleeterror.NewInvalidArgumentError("UploadCommandArtifactRequest after header must be chunk")
		}
		if len(chunk.GetData()) > commandArtifactChunkSize {
			return 0, fleeterror.NewInvalidArgumentErrorf("command artifact chunk exceeds %d bytes", commandArtifactChunkSize)
		}
		r.buf = chunk.GetData()
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

type commandArtifactUploadReceive struct {
	msg *pb.UploadCommandArtifactRequest
	err error
}

type commandArtifactUploadReceiver struct {
	stream   *connect.ClientStream[pb.UploadCommandArtifactRequest]
	receive  chan commandArtifactUploadReceive
	done     chan struct{}
	doneOnce sync.Once
}

func newCommandArtifactUploadReceiver(stream *connect.ClientStream[pb.UploadCommandArtifactRequest]) *commandArtifactUploadReceiver {
	r := &commandArtifactUploadReceiver{
		stream:  stream,
		receive: make(chan commandArtifactUploadReceive, 1),
		done:    make(chan struct{}),
	}
	go r.run()
	return r
}

func (r *commandArtifactUploadReceiver) run() {
	for {
		var result commandArtifactUploadReceive
		if !r.stream.Receive() {
			if err := r.stream.Err(); err != nil {
				result.err = fmt.Errorf("receive command artifact upload request: %w", err)
			} else {
				result.err = io.EOF
			}
		} else {
			result.msg = r.stream.Msg()
		}

		select {
		case r.receive <- result:
		case <-r.done:
			return
		}
		if result.err != nil {
			return
		}
	}
}

func (r *commandArtifactUploadReceiver) Close() {
	r.doneOnce.Do(func() {
		close(r.done)
	})
}

func (r *commandArtifactUploadReceiver) Receive(ctx context.Context, timeout time.Duration, label string) (*pb.UploadCommandArtifactRequest, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, contextConnectError(ctx.Err(), fmt.Sprintf("%s closed before receive completed", label))
	case <-timer.C:
		return nil, connect.NewError(connect.CodeDeadlineExceeded, fmt.Errorf("%s not received within %s", label, timeout))
	case got := <-r.receive:
		return got.msg, got.err
	}
}

func contextConnectError(err error, message string) error {
	code := connect.CodeDeadlineExceeded
	if errors.Is(err, context.Canceled) {
		code = connect.CodeCanceled
	}
	return connect.NewError(code, fmt.Errorf("%s: %w", message, err))
}

func commandArtifactRef(info *files.CommandArtifactInfo, purpose pb.CommandArtifactPurpose) *pb.CommandArtifactRef {
	return &pb.CommandArtifactRef{
		ArtifactId: info.ID,
		Purpose:    purpose,
		Filename:   info.Filename,
		SizeBytes:  info.Size,
		Sha256:     info.SHA256,
	}
}

func mapArtifactAdmissionError(err error) error {
	switch {
	case errors.Is(err, control.ErrArtifactAlreadyTransferred):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.Is(err, control.ErrArtifactTransferLimitExceeded):
		return connect.NewError(connect.CodeResourceExhausted, err)
	case errors.Is(err, control.ErrArtifactTransferAttemptsExceeded):
		return connect.NewError(connect.CodeResourceExhausted, err)
	case errors.Is(err, control.ErrArtifactNotExpected):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, control.ErrNoActiveStream):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	default:
		return fleeterror.NewFailedPreconditionError("command artifact transfer does not match an in-flight server-issued command")
	}
}

func (h *Handler) ReportDiscoveredDevices(ctx context.Context, req *connect.Request[pb.ReportDiscoveredDevicesRequest]) (*connect.Response[pb.ReportDiscoveredDevicesResponse], error) {
	subject, err := auth.GetSubject(ctx)
	if err != nil {
		return nil, err
	}
	commandID := req.Msg.GetCommandId()
	if commandID == "" {
		return nil, fleeterror.NewFailedPreconditionError("discovery report requires a command_id from a server-issued ControlCommand")
	}
	in := req.Msg.GetDevices()
	// Bind to the in-flight command and reserve quota so an agent can't stream
	// unbounded batches against one command_id.
	if admitErr := h.registry.AdmitReport(subject.FleetNodeID, commandID, len(in), control.ReportKindDiscovery); admitErr != nil {
		if errors.Is(admitErr, control.ErrReportQuotaExceeded) {
			return nil, connect.NewError(connect.CodeResourceExhausted, admitErr)
		}
		return nil, fleeterror.NewFailedPreconditionError("discovery report does not match an in-flight server-issued command")
	}

	// Drop devices outside the command's requested scan scope so a compromised
	// node can't report (or claim) devices it was never asked to scan. A nil
	// scope is unconstrained; ok is false only if the command was torn down
	// between AdmitReport and here (then nothing is in scope).
	scope, ok := h.registry.ReportScopeFor(subject.FleetNodeID, commandID)
	inScope := make([]*pb.DiscoveredDeviceReport, 0, len(in))
	var outOfScope int64
	for _, d := range in {
		if ok && (scope == nil || scope(d.GetIpAddress(), d.GetPort())) {
			inScope = append(inScope, d)
		} else {
			outOfScope++
		}
	}

	reports := make([]pairing.DiscoveredDeviceReport, 0, len(inScope))
	for _, d := range inScope {
		reports = append(reports, pairing.DiscoveredDeviceReport{
			DeviceIdentifier: d.GetDeviceIdentifier(),
			IPAddress:        d.GetIpAddress(),
			Port:             d.GetPort(),
			URLScheme:        d.GetUrlScheme(),
			DriverName:       d.GetDriverName(),
			Model:            d.GetModel(),
			Manufacturer:     d.GetManufacturer(),
			FirmwareVersion:  d.GetFirmwareVersion(),
		})
	}
	acceptedIdx, ownershipRejected, err := h.pairing.UpsertDiscoveredDevices(ctx, subject.FleetNodeID, subject.OrgID, reports)
	if err != nil {
		return nil, err
	}
	if outOfScope > 0 || ownershipRejected > 0 {
		slog.Warn("fleet node reported devices that were dropped",
			"fleet_node_id", subject.FleetNodeID,
			"org_id", subject.OrgID,
			"out_of_scope", outOfScope,
			"ownership_rejected", ownershipRejected,
		)
	}
	if len(acceptedIdx) > 0 {
		// Forward only store-accepted devices; out-of-scope and
		// ownership/attribution-rejected rows must not surface to the operator.
		batch := &pairingpb.DiscoverResponse{Devices: make([]*pairingpb.Device, 0, len(acceptedIdx))}
		for _, i := range acceptedIdx {
			batch.Devices = append(batch.Devices, toPairingDevice(inScope[i]))
		}
		h.registry.PublishBatch(subject.FleetNodeID, commandID, batch)
	}
	return connect.NewResponse(&pb.ReportDiscoveredDevicesResponse{
		AcceptedCount: int64(len(acceptedIdx)),
		RejectedCount: ownershipRejected + outOfScope,
	}), nil
}

func (h *Handler) ReportPairedDevices(ctx context.Context, req *connect.Request[pb.ReportPairedDevicesRequest]) (*connect.Response[pb.ReportPairedDevicesResponse], error) {
	subject, err := auth.GetSubject(ctx)
	if err != nil {
		return nil, err
	}
	commandID := req.Msg.GetCommandId()
	if commandID == "" {
		return nil, fleeterror.NewFailedPreconditionError("pairing report requires a command_id from a server-issued ControlCommand")
	}
	results := req.Msg.GetResults()
	// Admit, scope to the dispatched targets (consuming each to bar replay), then
	// persist authoritatively here -- the node-authenticated path is the source of
	// truth, so a disconnected operator can't lose a paired miner.
	kept, meta, admitErr := h.registry.AdmitAndScopePairResults(subject.FleetNodeID, commandID, results)
	if admitErr != nil {
		switch {
		case errors.Is(admitErr, control.ErrReportQuotaExceeded):
			return nil, connect.NewError(connect.CodeResourceExhausted, admitErr)
		case errors.Is(admitErr, control.ErrEmptyReport):
			return nil, fleeterror.NewInvalidArgumentError("pairing report carried no results")
		default:
			return nil, fleeterror.NewFailedPreconditionError("pairing report does not match an in-flight server-issued command")
		}
	}

	persisted := make([]*pb.FleetNodePairResult, 0, len(kept))
	var persistFailed []string
	for _, r := range kept {
		status, err := h.pairing.PersistFleetNodePairResult(ctx, subject.FleetNodeID, meta.OrgID, r, meta.AssignedBy)
		if err != nil {
			// Per-device isolation: one persist failure must not drop the others
			// (already paired on the node). A failed result isn't forwarded as paired;
			// the operator synthesizes a terminal FAILED so it surfaces for re-issue.
			slog.Error("failed to persist fleet node pair result",
				"fleet_node_id", subject.FleetNodeID, "device_identifier", r.GetDeviceIdentifier(), "err", err)
			persistFailed = append(persistFailed, r.GetDeviceIdentifier())
			continue
		}
		// Forward the persisted status, not the raw report: a stale AUTH_NEEDED for
		// an already-PAIRED device persists as PAIRED, so the operator must see PAIRED.
		r.Outcome = pairOutcomeForStatus(status)
		r.DefaultPasswordActive = defaultPasswordActiveForStatus(status)
		if pairOutcomeSucceeded(status) {
			r.ErrorMessage = ""
		}
		persisted = append(persisted, r)
	}
	// Admission consumed these targets; return them so a retried report for the
	// same command can persist after a transient failure.
	if len(persistFailed) > 0 {
		h.registry.ReinstatePairTargets(subject.FleetNodeID, commandID, persistFailed)
	}

	// Forward only persisted results for live display; lossy is fine now that
	// persistence above is authoritative, like discovery's PublishBatch.
	if len(persisted) > 0 {
		h.registry.PublishPairResults(subject.FleetNodeID, commandID, persisted)
	}
	return connect.NewResponse(&pb.ReportPairedDevicesResponse{
		AcceptedCount: int64(len(persisted)),
		RejectedCount: int64(len(results) - len(persisted)),
	}), nil
}

// pairOutcomeForStatus maps the persisted device_pairing status back to the pair
// outcome forwarded to the operator, so the live display reflects what was stored
// (paired-like / AUTHENTICATION_NEEDED / FAILED) rather than the raw node report.
func pairOutcomeForStatus(status string) pb.PairOutcome {
	switch status {
	case pairing.StatusPaired, pairing.StatusDefaultPassword:
		return pb.PairOutcome_PAIR_OUTCOME_PAIRED
	case pairing.StatusAuthenticationNeeded:
		return pb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED
	default:
		return pb.PairOutcome_PAIR_OUTCOME_ERROR
	}
}

func pairOutcomeSucceeded(status string) bool {
	return status == pairing.StatusPaired || status == pairing.StatusDefaultPassword
}

func defaultPasswordActiveForStatus(status string) *bool {
	if status == pairing.StatusDefaultPassword {
		active := true
		return &active
	}
	return nil
}

func toPairingDevice(d *pb.DiscoveredDeviceReport) *pairingpb.Device {
	return &pairingpb.Device{
		DeviceIdentifier: d.GetDeviceIdentifier(),
		IpAddress:        d.GetIpAddress(),
		Port:             d.GetPort(),
		UrlScheme:        d.GetUrlScheme(),
		DriverName:       d.GetDriverName(),
		Model:            d.GetModel(),
		Manufacturer:     d.GetManufacturer(),
		FirmwareVersion:  d.GetFirmwareVersion(),
	}
}

// HelloTimeout bounds the wait for the agent's first Hello, so a node that
// opens the stream and never sends one can't pin a goroutine + HTTP/2 stream
// indefinitely. Var so tests can shrink it.
var HelloTimeout = 5 * time.Second

func (h *Handler) ControlStream(ctx context.Context, stream *connect.BidiStream[pb.ControlStreamRequest, pb.ControlStreamResponse]) error {
	subject, err := auth.GetSubject(ctx)
	if err != nil {
		return err
	}

	// streamMsg carries a blocking stream.Receive() result out of the reader
	// goroutine so the selects below can multiplex it against timeouts/commands.
	type streamMsg struct {
		msg *pb.ControlStreamRequest
		err error
	}
	helloCh := make(chan streamMsg, 1)
	go func() {
		msg, err := stream.Receive()
		helloCh <- streamMsg{msg: msg, err: err}
	}()

	// NewTimer + Stop (not time.After) releases the timer once Hello arrives,
	// instead of lingering until HelloTimeout on every successful connection.
	helloTimer := time.NewTimer(HelloTimeout)
	defer helloTimer.Stop()
	var first *pb.ControlStreamRequest
	select {
	case <-helloTimer.C:
		return fleeterror.NewFailedPreconditionErrorf("control stream Hello not received within %s", HelloTimeout)
	case <-ctx.Done():
		return fleeterror.NewInternalErrorf("control stream closed before hello: %v", ctx.Err())
	case r := <-helloCh:
		if r.err != nil {
			return fleeterror.NewInvalidArgumentErrorf("control stream closed before hello: %v", r.err)
		}
		first = r.msg
	}
	if first.GetHello() == nil {
		return fleeterror.NewInvalidArgumentError("first ControlStreamRequest must be Hello")
	}

	regHandle := h.registry.Register(subject.FleetNodeID)
	defer regHandle.Unregister()

	if sendErr := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Accepted{
		Accepted: &pb.ControlAccepted{ServerTime: timestamppb.New(time.Now().UTC())},
	}}); sendErr != nil {
		return fleeterror.NewInternalErrorf("send accepted: %v", sendErr)
	}

	// Side-goroutine bridges blocking stream.Receive into the select loop. Its
	// send selects on regHandle.Done (closed by the deferred Unregister) so it
	// can't block forever on a full channel after the main loop exits.
	incoming := make(chan streamMsg, 2)
	go func() {
		for {
			msg, err := stream.Receive()
			select {
			case incoming <- streamMsg{msg: msg, err: err}:
			case <-regHandle.Done:
				return
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-regHandle.Done:
			// Newest-wins eviction or Unregister fired; let the handler
			// exit so connect-go closes the stream.
			return nil
		case cmd := <-regHandle.Outgoing:
			if sendErr := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Command{Command: cmd}}); sendErr != nil {
				return fleeterror.NewInternalErrorf("send command: %v", sendErr)
			}
		case r := <-incoming:
			if r.err != nil {
				if errors.Is(r.err, io.EOF) {
					return nil
				}
				return fleeterror.NewInternalErrorf("control stream recv: %v", r.err)
			}
			if ack := r.msg.GetAck(); ack != nil {
				// Trust boundary for node input: drop a malformed/oversized ack rather than
				// routing it into the command error path (the waiting command times out).
				if vErr := protovalidate.Validate(ack); vErr != nil {
					slog.Warn("dropping invalid ControlAck from fleet node",
						"fleet_node_id", subject.FleetNodeID, "err", vErr)
					continue
				}
				regHandle.PublishAck(ack)
			}
		}
	}
}
