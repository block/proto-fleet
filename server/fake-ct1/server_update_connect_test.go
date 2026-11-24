package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2/mdkv2connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
)

// connectUpdateClient serves pair member i over HTTP and returns a browser-style
// Connect client for its ServerUpdateService.
func (h *updateHarness) connectUpdateClient(i int) mdkv2connect.ServerUpdateServiceClient {
	h.t.Helper()
	handler, cleanup, err := newHTTPHandler(h.states[i], "")
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(cleanup)
	srv := httptest.NewServer(handler)
	h.t.Cleanup(srv.Close)
	return mdkv2connect.NewServerUpdateServiceClient(http.DefaultClient, srv.URL, connect.WithProtoJSON())
}

func connectReason(t *testing.T, err error) (connect.Code, string) {
	t.Helper()
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected a Connect error, got %v", err)
	}
	for _, detail := range connectErr.Details() {
		value, detailErr := detail.Value()
		if info, ok := value.(*errdetails.ErrorInfo); detailErr == nil && ok && info.GetDomain() == frigateRefusalDomain {
			return connectErr.Code(), info.GetReason()
		}
	}
	return connectErr.Code(), ""
}

func connectRequest[T any](msg *T, headers ...string) *connect.Request[T] {
	req := connect.NewRequest(msg)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header().Set(headers[i], headers[i+1])
	}
	return req
}

func TestConnectServerUpdateLifecycle(t *testing.T) {
	h := newUpdateHarness(t, nil)
	client := h.connectUpdateClient(1)
	ctx := context.Background()

	// The exact-instance header selects which member answers support.
	for _, id := range []string{"ct1-a", "ct1-b"} {
		support, err := client.GetServerUpdateSupport(ctx, connectRequest(&mdk.GetServerUpdateSupportRequest{}, serverIDHeader, id))
		if err != nil {
			t.Fatal(err)
		}
		if support.Msg.GetInstanceId() != id {
			t.Fatalf("support for %s answered by %s", id, support.Msg.GetInstanceId())
		}
	}

	// Stage both members through one member's Connect gateway.
	pkg := testPackage(1500)
	fingerprints := map[string]string{}
	for _, id := range []string{"ct1-a", "ct1-b"} {
		begin, err := client.BeginServerUpdateStage(ctx, connectRequest(&mdk.BeginServerUpdateStageRequest{Manifest: h.manifest("job-connect", id, pkg)}, serverIDHeader, id))
		if err != nil {
			t.Fatal(err)
		}
		fingerprint := begin.Msg.GetManifestSha256()
		if _, err := client.AppendServerUpdateStage(ctx, connectRequest(&mdk.AppendServerUpdateStageRequest{JobId: "job-connect", ManifestSha256: fingerprint, Chunk: pkg}, serverIDHeader, id)); err != nil {
			t.Fatal(err)
		}
		if _, err := client.CommitServerUpdateStage(ctx, connectRequest(&mdk.CommitServerUpdateStageRequest{JobId: "job-connect", ManifestSha256: fingerprint}, serverIDHeader, id)); err != nil {
			t.Fatal(err)
		}
		fingerprints[id] = fingerprint
	}

	// A refusal keeps its Frigate ErrorInfo reason over Connect.
	h.sim.setFaults(func(f *updateFaults) { f.FirmwareActive = true })
	_, err := client.StartServerUpdate(ctx, connectRequest(startRequest("job-connect", fingerprints)))
	if code, reason := connectReason(t, err); code != connect.CodeFailedPrecondition || reason != "UPDATE_FIRMWARE_ACTIVE" {
		t.Fatalf("refusal = %s/%s: %v", code, reason, err)
	}
	h.sim.setFaults(func(f *updateFaults) { f.FirmwareActive = false })

	started, err := client.StartServerUpdate(ctx, connectRequest(startRequest("job-connect", fingerprints), actorHeader, "proto-os"))
	if err != nil {
		t.Fatal(err)
	}
	if started.Msg.GetRollout().GetActor() != "proto-os" {
		t.Fatalf("actor = %q, want proto-os", started.Msg.GetRollout().GetActor())
	}
	h.waitPhase("job-connect", mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_SUCCEEDED)
	final, err := client.GetServerUpdateStatus(ctx, connectRequest(&mdk.GetServerUpdateStatusRequest{JobId: "job-connect"}))
	if err != nil || final.Msg.GetRollout().GetPhase() != mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_SUCCEEDED {
		t.Fatalf("final status = %v, %v", final, err)
	}

	_, err = client.GetServerUpdateStatus(ctx, connectRequest(&mdk.GetServerUpdateStatusRequest{JobId: "never-started"}))
	if code, reason := connectReason(t, err); code != connect.CodeNotFound || reason != "UPDATE_NOT_FOUND" {
		t.Fatalf("unknown job = %s/%s: %v", code, reason, err)
	}
}

func TestConnectMaintenanceServesOnlyUpdateRecoveryCalls(t *testing.T) {
	h := newUpdateHarness(t, nil)
	handler, cleanup, err := newHTTPHandler(h.states[0], "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	update := mdkv2connect.NewServerUpdateServiceClient(http.DefaultClient, srv.URL)
	system := mdkv2connect.NewSystemServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()

	h.sim.mu.Lock()
	h.sim.maintenance = true
	h.sim.mu.Unlock()

	_, err = update.GetServerUpdateStatus(ctx, connectRequest(&mdk.GetServerUpdateStatusRequest{}))
	if code, reason := connectReason(t, err); code != connect.CodeNotFound || reason != "UPDATE_NOT_FOUND" {
		t.Fatalf("status during maintenance = %s/%s: %v", code, reason, err)
	}
	_, err = update.BeginServerUpdateStage(ctx, connectRequest(&mdk.BeginServerUpdateStageRequest{Manifest: h.manifest("job", "ct1-a", testPackage(10))}))
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("staging during maintenance = %v, want Unavailable", err)
	}
	_, err = system.GetSystemInfo(ctx, connectRequest(&mdk.GetSystemInfoRequest{}))
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("system info during maintenance = %v, want Unavailable", err)
	}
}
