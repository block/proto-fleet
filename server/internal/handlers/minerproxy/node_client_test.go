package minerproxy

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

func nodeWebRequest(method, path string) *pb.MinerWebRequest {
	return &pb.MinerWebRequest{
		Target: &pb.MinerConnectionDescriptor{DeviceClass: 1, DeviceIdentifier: "rig", DriverName: "proto", IpAddress: "10.0.0.10", Port: "80", UrlScheme: "http", SerialNumber: "rig-serial", CredentialPassword: []byte("sealed-password")},
		Method: method, Path: path, Timeout: durationpb.New(25 * time.Second),
	}
}

func nodeWebTestClient(t *testing.T, handler http.HandlerFunc) *NodeClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewNodeClient(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	})
}

func TestNodeClientExecutionBudgetStartsOnArrival(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		request := nodeWebRequest(http.MethodGet, "/api/v1/system")
		// Delivery takes longer than the execution budget. It must not consume it.
		time.Sleep(40 * time.Second)
		budgets := make(chan time.Duration, 1)
		client := NewNodeClient(nil)
		client.transport.httpClient.Transport = proxyRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			deadline, _ := r.Context().Deadline()
			budgets <- time.Until(deadline)
			<-r.Context().Done()
			return nil, r.Context().Err()
		})
		response, err := client.Forward(t.Context(), request, func(context.Context) (string, error) { return "password", nil })
		if response != nil {
			_ = response.Body.Close()
		}
		require.Error(t, err, "the unresponsive miner must still time out")
		require.Len(t, budgets, 1, "a delayed request must reach the miner")
		require.Equal(t, 25*time.Second, <-budgets)
	})
}

func TestNodeClientPreservesControlsAndKeepsCredentialsLocal(t *testing.T) {
	var loginCount atomic.Int32
	var seenBody string
	client := nodeWebTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("Cookie"))
		require.NotEqual(t, "Bearer fleet-token", r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/api/v1/pairing/info":
			require.Empty(t, r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, `{"cb_sn":"rig-serial","mac":"aa:bb:cc:dd:ee:ff"}`)
		case minerLoginPath:
			loginCount.Add(1)
			var login loginRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&login))
			require.Equal(t, "miner-password", login.Password)
			_, _ = io.WriteString(w, `{"access_token":"miner-token"}`)
		default:
			require.Equal(t, "Bearer miner-token", r.Header.Get("Authorization"))
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			seenBody = string(body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":"test device rejection"}`)
		}
	})
	request := nodeWebRequest(http.MethodPut, "/api/v1/mining/target")
	request.Body = []byte(`{"power_target_watts":3000,"balance_bays":true}`)
	request.Headers = []*pb.MinerWebHeader{{Name: "Cookie", Value: "session=fleet"}, {Name: "Authorization", Value: "Bearer fleet-token"}, {Name: "Content-Type", Value: "application/json"}}
	password := func(context.Context) (string, error) { return "miner-password", nil }
	for range 2 {
		response, err := client.Forward(t.Context(), request, password)
		require.NoError(t, err)
		require.Equal(t, http.StatusConflict, response.StatusCode)
		got, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		require.JSONEq(t, `{"error":"test device rejection"}`, string(got))
		require.JSONEq(t, string(request.Body), seenBody)
	}
	require.EqualValues(t, 1, loginCount.Load(), "cache auth across polling")
	request.Target.CredentialPassword = []byte("rotated-sealed-password")
	response, err := client.Forward(t.Context(), request, password)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.EqualValues(t, 2, loginCount.Load(), "credential changes invalidate auth")
}

func TestNodeClientRejectsUnsafeRequestsBeforeAnyIO(t *testing.T) {
	client := nodeWebTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected miner I/O") })
	mutations := []func(*pb.MinerWebRequest){
		func(r *pb.MinerWebRequest) { r.Target.IpAddress = "127.0.0.1" },
		func(r *pb.MinerWebRequest) { r.Target.DriverName = "antminer" },
		func(r *pb.MinerWebRequest) { r.Path = "http://10.0.0.1/api/v1/system" },
		func(r *pb.MinerWebRequest) { r.Path = "/api/v1/../system" },
		func(r *pb.MinerWebRequest) { r.Path = "/api/v1/auth/password" },
		func(r *pb.MinerWebRequest) { r.Path = "/api/v1/pools/1"; r.Method = http.MethodPut },
		func(r *pb.MinerWebRequest) { r.Timeout = durationpb.New(-time.Second) },
		func(r *pb.MinerWebRequest) { r.Timeout = nil },
		func(r *pb.MinerWebRequest) { r.Timeout = durationpb.New(0) },
		func(r *pb.MinerWebRequest) { r.Timeout = durationpb.New(time.Minute) },
	}
	for _, mutate := range mutations {
		request := nodeWebRequest(http.MethodGet, "/api/v1/system")
		mutate(request)
		response, err := client.Forward(t.Context(), request, func(context.Context) (string, error) { t.Error("unexpected secret access"); return "", nil })
		if response != nil {
			require.NoError(t, response.Body.Close())
		}
		require.Error(t, err)
	}
}

func TestNodeClientChecksIdentityBeforeCredentials(t *testing.T) {
	client := nodeWebTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/pairing/info", r.URL.Path)
		_, _ = io.WriteString(w, `{"cb_sn":"different-rig"}`)
	})
	request := nodeWebRequest(http.MethodPost, "/api/v1/mining/start")
	response, err := client.Forward(t.Context(), request, func(context.Context) (string, error) { t.Error("unexpected secret access"); return "", nil })
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
	require.ErrorContains(t, err, "identity")
}

func TestNodeClientIncludesReadPostAndRejectsEncodedPaths(t *testing.T) {
	request := nodeWebRequest(http.MethodPost, "/api/v1/timeseries")
	require.NoError(t, ValidateNodeWebRequest(request))
	for _, path := range []string{"/api/v1/%2e%2e/system", "/api/v1/system?other=1", "/api/v1/system#fragment", "/api/v1/system%2flogs"} {
		invalid := proto.CloneOf(request)
		invalid.Path = path
		require.Error(t, ValidateNodeWebRequest(invalid), path)
	}
	request.Query = strings.Repeat("x", 8193)
	require.Error(t, ValidateNodeWebRequest(request))
}
