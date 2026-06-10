package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	minercommandv1 "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pairingv1 "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/urfave/cli/v3"
	"google.golang.org/protobuf/proto"
)

func TestBuildDiscoverRequestFromIPFlags(t *testing.T) {
	got, err := buildDiscoverRequest([]string{"127.0.0.1", "10.0.0.2"}, []string{"8080", "4433"}, "")
	if err != nil {
		t.Fatalf("buildDiscoverRequest() error = %v", err)
	}

	want := &pairingv1.DiscoverRequest{
		Mode: &pairingv1.DiscoverRequest_IpList{
			IpList: &pairingv1.IPListModeRequest{
				IpAddresses: []string{"127.0.0.1", "10.0.0.2"},
				Ports:       []string{"8080", "4433"},
			},
		},
	}
	if !proto.Equal(want, got) {
		t.Fatalf("buildDiscoverRequest() = %v, want %v", got, want)
	}
}

func TestBuildDiscoverRequestRejectsJSONWithIPFlags(t *testing.T) {
	if _, err := buildDiscoverRequest([]string{"127.0.0.1"}, nil, "discover.json"); err == nil {
		t.Fatal("buildDiscoverRequest() error = nil, want conflicting input mode error")
	}
	if _, err := buildDiscoverRequest(nil, []string{"8080"}, "discover.json"); err == nil {
		t.Fatal("buildDiscoverRequest() error = nil, want conflicting input mode error")
	}
}

func TestBuildDiscoverRequestRequiresInputMode(t *testing.T) {
	if _, err := buildDiscoverRequest(nil, nil, ""); err == nil {
		t.Fatal("buildDiscoverRequest() error = nil, want missing input mode error")
	}
}

func TestBuildDiscoverRequestFromJSONFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "discover.json")
	contents := `{"ip_range": {"start_ip": "10.0.0.1", "end_ip": "10.0.0.9", "ports": ["8080"]}}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write request file: %v", err)
	}

	got, err := buildDiscoverRequest(nil, nil, path)
	if err != nil {
		t.Fatalf("buildDiscoverRequest() error = %v", err)
	}

	want := &pairingv1.DiscoverRequest{
		Mode: &pairingv1.DiscoverRequest_IpRange{
			IpRange: &pairingv1.IPRangeModeRequest{
				StartIp: "10.0.0.1",
				EndIp:   "10.0.0.9",
				Ports:   []string{"8080"},
			},
		},
	}
	if !proto.Equal(want, got) {
		t.Fatalf("buildDiscoverRequest() = %v, want %v", got, want)
	}
}

func TestBuildDiscoverRequestFromStdin(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	if _, err := writer.WriteString(`{"mdns": {"service_type": "_fleet._tcp", "domain": "local", "timeout_seconds": 5}}`); err != nil {
		t.Fatalf("write stdin payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	originalStdin := os.Stdin
	os.Stdin = reader
	defer func() { os.Stdin = originalStdin }()

	got, err := buildDiscoverRequest(nil, nil, "-")
	if err != nil {
		t.Fatalf("buildDiscoverRequest() error = %v", err)
	}

	want := &pairingv1.DiscoverRequest{
		Mode: &pairingv1.DiscoverRequest_Mdns{
			Mdns: &pairingv1.MDNSModeRequest{
				ServiceType:    "_fleet._tcp",
				Domain:         "local",
				TimeoutSeconds: 5,
			},
		},
	}
	if !proto.Equal(want, got) {
		t.Fatalf("buildDiscoverRequest() = %v, want %v", got, want)
	}
}

func TestBuildDiscoverRequestRejectsJSONWithoutMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "discover.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write request file: %v", err)
	}

	if _, err := buildDiscoverRequest(nil, nil, path); err == nil {
		t.Fatal("buildDiscoverRequest() error = nil, want missing mode error")
	}
}

// buildPairRequestFromArgs parses pair command flags from args and returns the
// result of buildPairRequest. The nil client is safe because --device
// selectors resolve without RPC calls.
func buildPairRequestFromArgs(t *testing.T, args ...string) (*pairingv1.PairRequest, error) {
	t.Helper()

	var req *pairingv1.PairRequest
	var buildErr error
	cmd := &cli.Command{
		Name:  "pair",
		Flags: pairingPairFlags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			req, buildErr = buildPairRequest(ctx, cmd, nil)
			return nil
		},
	}
	if err := cmd.Run(context.Background(), append([]string{"pair"}, args...)); err != nil {
		t.Fatalf("run pair flag harness: %v", err)
	}
	return req, buildErr
}

func TestBuildPairRequestFromDeviceFlag(t *testing.T) {
	got, err := buildPairRequestFromArgs(t, "--device", "device-1")
	if err != nil {
		t.Fatalf("buildPairRequest() error = %v", err)
	}

	want := &pairingv1.PairRequest{
		DeviceSelector: &minercommandv1.DeviceSelector{
			SelectionType: &minercommandv1.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{DeviceIdentifiers: []string{"device-1"}},
			},
		},
	}
	if !proto.Equal(want, got) {
		t.Fatalf("buildPairRequest() = %v, want %v", got, want)
	}
}

func TestBuildPairRequestIncludesCredentialsOnlyWhenProvided(t *testing.T) {
	withoutCredentials, err := buildPairRequestFromArgs(t, "--device", "device-1")
	if err != nil {
		t.Fatalf("buildPairRequest() error = %v", err)
	}
	if withoutCredentials.GetCredentials() != nil {
		t.Fatalf("buildPairRequest() credentials = %v, want nil", withoutCredentials.GetCredentials())
	}

	withCredentials, err := buildPairRequestFromArgs(t,
		"--device", "device-1",
		"--device-username", "root",
		"--device-password", "secret",
	)
	if err != nil {
		t.Fatalf("buildPairRequest() error = %v", err)
	}
	wantCredentials := &pairingv1.Credentials{
		Username: "root",
		Password: proto.String("secret"),
	}
	if !proto.Equal(wantCredentials, withCredentials.GetCredentials()) {
		t.Fatalf("buildPairRequest() credentials = %v, want %v", withCredentials.GetCredentials(), wantCredentials)
	}

	usernameOnly, err := buildPairRequestFromArgs(t, "--device", "device-1", "--device-username", "root")
	if err != nil {
		t.Fatalf("buildPairRequest() error = %v", err)
	}
	if usernameOnly.GetCredentials().GetUsername() != "root" {
		t.Fatalf("buildPairRequest() username = %q, want %q", usernameOnly.GetCredentials().GetUsername(), "root")
	}
	if usernameOnly.GetCredentials().Password != nil {
		t.Fatalf("buildPairRequest() password = %v, want nil", usernameOnly.GetCredentials().Password)
	}
}

func TestBuildPairRequestRejectsMissingSelector(t *testing.T) {
	_, err := buildPairRequestFromArgs(t)
	if err == nil {
		t.Fatal("buildPairRequest() error = nil, want missing selector error")
	}
	if !strings.Contains(err.Error(), "--all-devices") {
		t.Fatalf("buildPairRequest() error = %v, want selector requirement message", err)
	}
}
