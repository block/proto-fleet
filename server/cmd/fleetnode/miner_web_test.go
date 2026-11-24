package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/handlers/minerproxy"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

type webFrameCapture struct{ frames []*pb.MinerWebResponseChunk }

func (c *webFrameCapture) Send(request *pb.ControlStreamRequest) error {
	c.frames = append(c.frames, request.GetMinerWebResponse())
	return nil
}

func TestMinerWebResponseChunksAndHeaderPolicy(t *testing.T) {
	data := strings.Repeat("logs", 40000)
	capture := &webFrameCapture{}
	response := &http.Response{StatusCode: http.StatusConflict, Header: http.Header{"Content-Type": {"text/plain"}, "Set-Cookie": {"secret"}, "Location": {"http://unsafe"}}, Body: io.NopCloser(strings.NewReader(data))}
	require.NoError(t, sendMinerWebResponse(capture, "command", response))
	require.EqualValues(t, 409, capture.frames[0].GetHead().GetStatusCode())
	require.Len(t, capture.frames[0].GetHead().GetHeaders(), 1)
	var got bytes.Buffer
	for _, frame := range capture.frames {
		require.Equal(t, "command", frame.CommandId)
		require.LessOrEqual(t, len(frame.GetData()), control.MinerWebChunkBytes)
		got.Write(frame.GetData())
	}
	require.Equal(t, data, got.String())
	require.True(t, capture.frames[len(capture.frames)-1].GetEnd())
}

type failedWebBody struct{}

func (failedWebBody) Read([]byte) (int, error) { return 0, errors.New("upstream truncated") }
func (failedWebBody) Close() error             { return nil }
func TestMinerWebTruncationNeverSendsEnd(t *testing.T) {
	capture := &webFrameCapture{}
	require.Error(t, sendMinerWebResponse(capture, "command", &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: failedWebBody{}}))
	require.Len(t, capture.frames, 1)
	require.False(t, capture.frames[0].GetEnd())
}

func TestMinerWebRejectsInvalidExecutionTimeout(t *testing.T) {
	for _, timeout := range []*durationpb.Duration{nil, durationpb.New(0), durationpb.New(-time.Second), durationpb.New(time.Minute)} {
		request := &pb.MinerWebRequest{Target: &pb.MinerConnectionDescriptor{DeviceIdentifier: "rig", DriverName: "proto", DeviceClass: 1, IpAddress: "10.0.0.1", Port: "80", UrlScheme: "http", SerialNumber: "serial"}, Method: "POST", Path: "/api/v1/mining/start", Timeout: timeout}
		capture := &captureAcker{}
		(&RunCmd{}).handleMinerWeb(t.Context(), capture, "invalid", request, slog.Default())
		require.Equal(t, pb.AckCode_ACK_CODE_BAD_REQUEST, capture.only(t).Code)
	}
}

// Exercise the actual control session and node HTTP client together: the server
// timestamp must not affect execution time, and credentials must be opened locally.
func TestControlLoop_MinerWebForwardsWithServerClockSkew(t *testing.T) {
	for _, skew := range []time.Duration{-time.Hour, time.Hour} {
		t.Run(skew.String(), func(t *testing.T) {
			const commandID = "miner-web-command"
			const requestBody = `{"power_target_watts":3000,"balance_bays":true}`
			responseBody := strings.Repeat("device rejected target\n", 8000)
			var identityChecks, logins atomic.Int32
			type rigRequest struct {
				method, path, query, body string
				headers                   http.Header
			}
			forwarded := make(chan rigRequest, 8)
			rig := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Empty(t, r.Header.Get("Cookie"))
				switch r.URL.Path {
				case "/api/v1/pairing/info":
					identityChecks.Add(1)
					assert.Empty(t, r.Header.Get("Authorization"))
					_, _ = io.WriteString(w, `{"cb_sn":"rig-serial"}`)
				case "/api/v1/auth/login":
					logins.Add(1)
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Empty(t, r.Header.Get("Authorization"))
					var login struct {
						Password string `json:"password"`
					}
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&login))
					assert.Equal(t, "node-local-password", login.Password)
					_, _ = io.WriteString(w, `{"access_token":"rig-token"}`)
				case "/api/v1/mining/target":
					body, err := io.ReadAll(r.Body)
					assert.NoError(t, err)
					forwarded <- rigRequest{r.Method, r.URL.Path, r.URL.RawQuery, string(body), r.Header.Clone()}
					w.Header().Set("Content-Type", "text/plain")
					w.Header().Set("Content-Disposition", `inline; filename="rig.txt"`)
					w.Header().Set("Set-Cookie", "miner-secret=hidden")
					w.Header().Set("Location", "http://unsafe")
					w.WriteHeader(http.StatusConflict)
					_, _ = io.WriteString(w, responseBody)
				default:
					t.Errorf("unexpected miner request: %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(rig.Close)

			codec := &credentialCodec{key: bytes.Repeat([]byte{1}, credentialKeySize)}
			sealed, err := codec.Seal(sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Username: "root", Password: "node-local-password"}})
			require.NoError(t, err)
			secrets := &minerWebTestSecrets{secretProvider: codec, opened: make(chan *pb.MinerConnectionDescriptor, 8)}
			request := &pb.MinerWebRequest{
				Target: &pb.MinerConnectionDescriptor{DeviceIdentifier: "rig", DriverName: "proto", DeviceClass: 1, IpAddress: "10.0.0.10", Port: "80", UrlScheme: "http", SerialNumber: "rig-serial", CredentialUsername: sealed.Username, CredentialPassword: sealed.Password},
				Method: http.MethodPut, Path: "/api/v1/mining/target", Query: "source=fleet",
				Body: []byte(requestBody), Timeout: durationpb.New(20 * time.Second),
				Headers: []*pb.MinerWebHeader{
					{Name: "Content-Type", Value: "application/json"},
					{Name: "Authorization", Value: "Bearer fleet-token"},
					{Name: "Cookie", Value: "fleet-session=hidden"},
				},
			}
			client := minerproxy.NewNodeClient(func(ctx context.Context, network, address string) (net.Conn, error) {
				assert.Equal(t, "10.0.0.10:80", address)
				return (&net.Dialer{}).DialContext(ctx, network, rig.Listener.Addr().String())
			})
			cmd := &RunCmd{webClient: client, minerSecrets: secrets}
			gateway := &controlFakeGateway{}
			gateway.setBehavior(controlFakeBehavior{serverTimeOffset: skew})
			gateway.queueWithID(commandID, mustMarshal(t, &pb.AgentCommand{Command: &pb.AgentCommand_MinerWeb{MinerWeb: request}}))

			runControlLoopOnce(t, cmd, gateway)

			acks := gateway.acksCopy()
			require.Len(t, acks, 1)
			require.Equal(t, commandID, acks[0].GetCommandId())
			require.Equal(t, pb.AckCode_ACK_CODE_OK, acks[0].GetCode(), "ack: %v", acks[0])
			require.True(t, acks[0].GetSucceeded())
			require.EqualValues(t, 1, logins.Load())
			require.Positive(t, identityChecks.Load())
			require.Len(t, secrets.opened, 1, "the node must open the opaque credential locally")
			require.True(t, proto.Equal(request.Target, <-secrets.opened))
			require.Len(t, forwarded, 1)
			gotRequest := <-forwarded
			require.Equal(t, http.MethodPut, gotRequest.method)
			require.Equal(t, request.Path, gotRequest.path)
			require.Equal(t, request.Query, gotRequest.query)
			require.JSONEq(t, requestBody, gotRequest.body)
			require.Equal(t, "application/json", gotRequest.headers.Get("Content-Type"))
			require.Equal(t, "Bearer rig-token", gotRequest.headers.Get("Authorization"))

			frames := gateway.commandFramesCopy()
			require.GreaterOrEqual(t, len(frames), 5, "head, chunked data, end, terminal ACK")
			head := frames[0].GetMinerWebResponse().GetHead()
			require.NotNil(t, head)
			require.EqualValues(t, http.StatusConflict, head.GetStatusCode())
			headers := make(http.Header)
			for _, h := range head.GetHeaders() {
				headers.Add(h.GetName(), h.GetValue())
			}
			require.Equal(t, "text/plain", headers.Get("Content-Type"))
			require.Equal(t, `inline; filename="rig.txt"`, headers.Get("Content-Disposition"))
			require.Empty(t, headers.Get("Set-Cookie"))
			require.Empty(t, headers.Get("Location"))
			var body bytes.Buffer
			for _, frame := range frames[1 : len(frames)-2] {
				chunk := frame.GetMinerWebResponse()
				require.NotNil(t, chunk)
				require.IsType(t, &pb.MinerWebResponseChunk_Data{}, chunk.GetPart())
				require.NotEmpty(t, chunk.GetData())
				require.LessOrEqual(t, len(chunk.GetData()), control.MinerWebChunkBytes)
				body.Write(chunk.GetData())
			}
			require.Equal(t, responseBody, body.String())
			require.True(t, frames[len(frames)-2].GetMinerWebResponse().GetEnd())
			require.Equal(t, acks[0], frames[len(frames)-1].GetAck())
			for _, frame := range frames[:len(frames)-1] {
				require.Equal(t, commandID, frame.GetMinerWebResponse().GetCommandId())
			}
		})
	}
}

type minerWebTestSecrets struct {
	secretProvider
	opened chan *pb.MinerConnectionDescriptor
}

func (s *minerWebTestSecrets) SecretBundle(target *pb.MinerConnectionDescriptor) (sdk.SecretBundle, error) {
	s.opened <- proto.CloneOf(target)
	return s.secretProvider.SecretBundle(target)
}
