package minerproxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/runtimepolicy"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestFleetNodeEmbeddedControlUsesAssignedNodeAndPreservesResponse(t *testing.T) {
	for _, outcome := range []string{"success", "disconnect", "busy", "busy-succeeded", "busy-after-head"} {
		t.Run(outcome, func(t *testing.T) {
			database, orgID, deviceID, h, original := proxyFenceFixture(t)
			q := sqlc.New(database.DB)
			fleetNode, err := q.CreateFleetNode(t.Context(), sqlc.CreateFleetNodeParams{OrgID: orgID, Name: "proxy-node", IdentityPubkey: []byte{1}, EncryptionPubkey: bytes.Repeat([]byte{1}, 32)})
			require.NoError(t, err)
			nodeID := fleetNode.ID
			rows, err := q.SetFleetNodeEnrollmentStatus(t.Context(), sqlc.SetFleetNodeEnrollmentStatusParams{ID: nodeID, OrgID: orgID, EnrollmentStatus: "CONFIRMED"})
			require.NoError(t, err)
			require.EqualValues(t, 1, rows)
			rows, err = q.PairDeviceToFleetNode(t.Context(), sqlc.PairDeviceToFleetNodeParams{FleetNodeID: nodeID, DeviceID: deviceID, OrgID: orgID})
			require.NoError(t, err)
			require.EqualValues(t, 1, rows)
			_, err = q.UpsertDiscoveredDevice(t.Context(), sqlc.UpsertDiscoveredDeviceParams{
				OrgID: orgID, DeviceIdentifier: original.deviceIdentifier, DriverName: "proto", DeviceClass: 1,
				IpAddress: "10.0.0.10", Port: "8080", UrlScheme: "http", IsActive: true,
				SerialNumber: original.binding.SerialNumber.String, MacAddress: original.binding.MacAddress,
			})
			require.NoError(t, err)
			rows, err = q.TransferDiscoveredDeviceAttribution(t.Context(), sqlc.TransferDiscoveredDeviceAttributionParams{FleetNodeID: nodeID, DeviceID: deviceID, OrgID: orgID})
			require.NoError(t, err)
			require.EqualValues(t, 1, rows)
			_, err = q.DeleteMinerCredentialsByDeviceIDAndOrgID(t.Context(), sqlc.DeleteMinerCredentialsByDeviceIDAndOrgIDParams{ID: deviceID, OrgID: orgID})
			require.NoError(t, err)
			target, err := h.resolveTarget(t.Context(), original.deviceIdentifier, orgID)
			require.NoError(t, err)
			require.Equal(t, runtimepolicy.MinerWebResponseTimeout, h.responseTimeout(target))
			registry := control.NewRegistry()
			h.nodeSender = registry
			stream, err := registry.RegisterAuthenticated(nodeID, "session", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
			require.NoError(t, err)
			defer stream.Unregister()
			direct := &countingProxyTransport{}
			h.httpClient = &http.Client{Transport: direct}
			node := nodeWebTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				require.Empty(t, r.Header.Get("Cookie"))
				if r.URL.Path == "/api/v1/pairing/info" {
					_, _ = fmt.Fprintf(w, `{"cb_sn":%q,"mac":%q}`, target.binding.SerialNumber.String, target.binding.MacAddress)
					return
				}
				require.Equal(t, "PUT", r.Method)
				require.Equal(t, "/api/v1/mining/target", r.URL.Path)
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.JSONEq(t, `{"power_target":1234}`, string(body))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"error":"fixture conflict"}`)
			})
			workerDone := make(chan struct{})
			go func() {
				defer close(workerDone)
				select {
				case command := <-stream.Outgoing:
					if outcome == "disconnect" {
						stream.Unregister()
						return
					}
					if strings.HasPrefix(outcome, "busy") {
						if outcome == "busy-after-head" {
							stream.PublishMinerWebResponse(&pb.MinerWebResponseChunk{CommandId: command.CommandId, Part: &pb.MinerWebResponseChunk_Head{Head: &pb.MinerWebResponseHead{StatusCode: 200}}})
						}
						stream.PublishAck(&pb.ControlAck{CommandId: command.CommandId, Code: pb.AckCode_ACK_CODE_BUSY, Succeeded: outcome == "busy-succeeded"})
						return
					}
					envelope := &pb.AgentCommand{}
					if err := proto.Unmarshal(command.Payload, envelope); err != nil {
						t.Error(err)
						return
					}
					if envelope.GetMinerWeb().GetTimeout().AsDuration() != runtimepolicy.MinerWebRequestTimeout {
						t.Error("caller deadline shortened the node execution budget")
					}
					response, err := node.Forward(t.Context(), envelope.GetMinerWeb(), nil)
					if err != nil {
						t.Error(err)
						stream.Unregister()
						return
					}
					defer response.Body.Close()
					body, err := io.ReadAll(response.Body)
					if err != nil {
						t.Error(err)
						return
					}
					stream.PublishMinerWebResponse(&pb.MinerWebResponseChunk{CommandId: command.CommandId, Part: &pb.MinerWebResponseChunk_Head{Head: &pb.MinerWebResponseHead{StatusCode: http.StatusConflict}}})
					stream.PublishMinerWebResponse(&pb.MinerWebResponseChunk{CommandId: command.CommandId, Part: &pb.MinerWebResponseChunk_Data{Data: body}})
					stream.PublishMinerWebResponse(&pb.MinerWebResponseChunk{CommandId: command.CommandId, Part: &pb.MinerWebResponseChunk_End{End: true}})
					stream.PublishAck(&pb.ControlAck{CommandId: command.CommandId, Code: pb.AckCode_ACK_CODE_OK, Succeeded: true})
				case <-time.After(5 * time.Second):
					t.Error("node did not receive command")
				}
			}()
			source := authenticatedProxyRequest(t, database, h, target)
			source.Method = http.MethodPut
			source.Body = io.NopCloser(strings.NewReader(`{"power_target":1234}`))
			source.SetPathValue("apiVersion", "v1")
			source.SetPathValue("rest", "mining/target")
			source.Header.Set("Origin", "http://fleet.test")
			ctx, cancel := context.WithTimeout(source.Context(), 10*time.Second)
			defer cancel()
			source = source.WithContext(ctx)
			recorder := &nodeProxyRecorder{newDeadlineProxyRecorder()}
			h.ServeHTTP(recorder, source)
			<-workerDone
			require.Zero(t, direct.calls.Load(), "cloud must never dial a node-owned miner")
			if outcome == "busy" {
				require.Equal(t, 502, recorder.Code)
				assertNoProxyAttempt(t, database, deviceID)
			} else if outcome != "success" {
				require.Equal(t, 502, recorder.Code)
				require.Equal(t, "unknown", proxyAttemptStatus(t, database, deviceID))
			} else {
				require.Equal(t, 409, recorder.Code)
				require.JSONEq(t, `{"error":"fixture conflict"}`, recorder.Body.String())
			}
		})
	}
}

type nodeProxyRecorder struct{ *deadlineProxyRecorder }

func (*nodeProxyRecorder) SetReadDeadline(time.Time) error { return nil }
