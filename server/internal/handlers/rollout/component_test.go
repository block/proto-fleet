package rollout

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domain "github.com/block/proto-fleet/server/internal/domain/rollout"
)

// The firmware component names which firmware of a model an assignment, a
// model group, a rollout or a history entry is about, so it must cross the
// contract in both directions.
func TestFirmwareComponentCrossesTheContract(t *testing.T) {
	t.Parallel()
	svc := newFakeService()
	svc.group.Component = domain.ComponentPSU
	svc.rollout.Component = domain.ComponentPSU
	svc.history = []domain.MinerFirmwareHistoryEntry{{
		RolloutID: 9, ChannelID: 3, ChannelName: "Canary", Manufacturer: "Proto", Model: "CT1", Component: domain.ComponentPSU,
		FirmwareVersion: "app=1410;bl=1110", FirmwareChecksum: checksum, RolloutStatus: domain.StatusActive, Phase: domain.PhaseInProgress,
	}}
	h := NewHandler(svc)
	ctx := ctxWithPermissions(t, authz.PermMinerFirmwareUpdate)
	assignments := []*pb.FirmwareAssignment{
		{Manufacturer: "Proto", Model: "CT1", FirmwareFileId: "fw-2"},
		{Manufacturer: "Proto", Model: "CT1", Component: "psu", FirmwareFileId: "psu-1"},
	}
	want := []domain.Assignment{
		{Manufacturer: "Proto", Model: "CT1", FirmwareFileID: "fw-2"},
		{Manufacturer: "Proto", Model: "CT1", Component: domain.ComponentPSU, FirmwareFileID: "psu-1"},
	}

	preview, err := h.PreviewReleaseChannelFirmware(ctx, connect.NewRequest(&pb.PreviewReleaseChannelFirmwareRequest{ChannelId: 3, Assignments: assignments[1:]}))
	require.NoError(t, err)
	assert.Equal(t, want[1:], svc.lastAssigned)
	require.Len(t, preview.Msg.Plans, 1)
	assert.Equal(t, "psu", preview.Msg.Plans[0].Component)

	applied, err := h.ApplyReleaseChannelFirmware(ctx, connect.NewRequest(&pb.ApplyReleaseChannelFirmwareRequest{ChannelId: 3, Assignments: assignments}))
	require.NoError(t, err)
	assert.Equal(t, want, svc.lastAssigned)
	require.Len(t, applied.Msg.StartedRollouts, 1)
	assert.Equal(t, "psu", applied.Msg.StartedRollouts[0].Component)

	groups, err := h.ListReleaseChannelModelGroups(ctx, connect.NewRequest(&pb.ListReleaseChannelModelGroupsRequest{ChannelId: 3}))
	require.NoError(t, err)
	require.Len(t, groups.Msg.ModelGroups, 1)
	assert.Equal(t, "psu", groups.Msg.ModelGroups[0].Component)

	_, err = h.ListReleaseChannelMiners(ctx, connect.NewRequest(&pb.ListReleaseChannelMinersRequest{ChannelId: 3, Manufacturer: "Proto", Model: "CT1", Component: "psu"}))
	require.NoError(t, err)
	assert.Equal(t, "psu", svc.lastComponent)
	_, err = h.ListReleaseChannelMiners(ctx, connect.NewRequest(&pb.ListReleaseChannelMinersRequest{ChannelId: 3}))
	require.NoError(t, err)
	assert.Empty(t, svc.lastComponent, "an omitted component is the main firmware")

	history, err := h.ListMinerFirmwareHistory(ctx, connect.NewRequest(&pb.ListMinerFirmwareHistoryRequest{DeviceIdentifier: "ct1"}))
	require.NoError(t, err)
	require.Len(t, history.Msg.Entries, 1)
	assert.Equal(t, "psu", history.Msg.Entries[0].Component)
}

// The REST resources map onto the same contract, so a component given as a
// query parameter or in a JSON body reaches the RPC unchanged.
func TestRESTCarriesTheFirmwareComponent(t *testing.T) {
	var miners pb.ListReleaseChannelMinersRequest
	var apply pb.ApplyReleaseChannelFirmwareRequest
	mux := http.NewServeMux()
	RegisterRESTRoutes(mux, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		switch r.URL.Path {
		case rolloutRPCPrefix + "ListReleaseChannelMiners":
			require.NoError(t, protojson.Unmarshal(body, &miners))
		case rolloutRPCPrefix + "ApplyReleaseChannelFirmware":
			require.NoError(t, protojson.Unmarshal(body, &apply))
		default:
			t.Errorf("unexpected RPC %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	send := func(method, path, body string) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	}

	send(http.MethodGet, "/api/v1/release-channels/3/miners?manufacturer=Proto&model=CT1&component=psu", "")
	assert.Equal(t, int64(3), miners.ChannelId)
	assert.Equal(t, "psu", miners.Component)

	send(http.MethodPost, "/api/v1/release-channels/3/firmware/apply",
		`{"assignments":[{"manufacturer":"Proto","model":"CT1","component":"psu","firmwareFileId":"psu-1"},{"manufacturer":"Proto","model":"CT1","firmwareFileId":"fw-2"}]}`)
	require.Len(t, apply.Assignments, 2)
	assert.Equal(t, "psu", apply.Assignments[0].Component)
	assert.Empty(t, apply.Assignments[1].Component)
	require.NoError(t, protovalidate.Validate(&apply), "the RPC validates the request the REST route builds")
}

// One request can change every firmware row of a model, so its assignments
// are unique per pair and component, not per pair. The RPC validates each
// request with these rules before the handler runs.
func TestAssignmentsAreUniquePerPairAndComponent(t *testing.T) {
	t.Parallel()
	hashboards := &pb.FirmwareAssignment{Manufacturer: "Proto", Model: "CT1", FirmwareFileId: "fw-2"}
	psu := &pb.FirmwareAssignment{Manufacturer: "Proto", Model: "CT1", Component: "psu", FirmwareFileId: "psu-1"}
	// The pair compares ASCII letters case-insensitively.
	otherCase := func(a *pb.FirmwareAssignment) *pb.FirmwareAssignment {
		return &pb.FirmwareAssignment{Manufacturer: "PROTO", Model: "ct1", Component: a.Component, FirmwareFileId: a.FirmwareFileId}
	}
	for _, tc := range []struct {
		name        string
		assignments []*pb.FirmwareAssignment
		valid       bool
	}{
		{"both components of one model", []*pb.FirmwareAssignment{hashboards, psu}, true},
		{"both components, then a clear of one", []*pb.FirmwareAssignment{hashboards, psu, {Manufacturer: "Proto", Model: "CT1", Component: "psu"}}, false},
		{"main firmware twice", []*pb.FirmwareAssignment{hashboards, otherCase(hashboards)}, false},
		{"PSU firmware twice", []*pb.FirmwareAssignment{psu, otherCase(psu)}, false},
		{"one component of two models", []*pb.FirmwareAssignment{psu, {Manufacturer: "Proto", Model: "CT2", Component: "psu", FirmwareFileId: "psu-2"}}, true},
	} {
		for name, request := range map[string]proto.Message{
			"apply":   &pb.ApplyReleaseChannelFirmwareRequest{ChannelId: 3, Assignments: tc.assignments},
			"preview": &pb.PreviewReleaseChannelFirmwareRequest{ChannelId: 3, Assignments: tc.assignments},
		} {
			err := protovalidate.Validate(request)
			assert.Equal(t, tc.valid, err == nil, "%s, %s: %v", tc.name, name, err)
			if !tc.valid {
				assert.ErrorContains(t, err, "at most one entry per manufacturer/model pair and component", "%s, %s", tc.name, name)
			}
		}
	}
}

// Only the components the server knows are valid in the contract.
func TestFirmwareComponentIsValidatedByTheContract(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		component string
		valid     bool
	}{{"", true}, {"psu", true}, {"PSU", false}, {"fan", false}, {" psu", false}} {
		err := protovalidate.Validate(&pb.FirmwareAssignment{Manufacturer: "Proto", Model: "CT1", Component: tc.component, FirmwareFileId: "file"})
		assert.Equal(t, tc.valid, err == nil, "component %q: %v", tc.component, err)
		err = protovalidate.Validate(&pb.ListReleaseChannelMinersRequest{ChannelId: 3, Component: tc.component})
		assert.Equal(t, tc.valid, err == nil, "component %q: %v", tc.component, err)
	}
}
