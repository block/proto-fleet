package gateway_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

func packageDownloadExpectation(artifact *pb.CommandArtifactRef) control.ArtifactExpectation {
	return control.ArtifactExpectation{
		Direction:        control.ArtifactDirectionDownload,
		Purpose:          pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_CONTROLLER_SOFTWARE_PACKAGE,
		ArtifactID:       artifact.GetArtifactId(),
		DeviceIdentifier: "miner-a",
	}
}

// storePackage publishes a controller software package as an upload does.
func storePackage(t *testing.T, store *files.FrigatePackageStore, payload []byte) uuid.UUID {
	t.Helper()
	id := uuid.New()
	staged, err := store.NewStagingPath(id.String())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(staged, payload, 0o600))
	require.NoError(t, store.Publish(id, staged))
	return id
}

func TestDownloadCommandArtifactServesControllerSoftwarePackage(t *testing.T) {
	payload := bytes.Repeat([]byte("controller software "), 4096)
	for _, tc := range []struct {
		name      string
		change    func(ref *pb.CommandArtifactRef)
		noStore   bool
		wantCode  connect.Code
		wantError string
	}{
		{name: "the granted package"},
		{name: "size no longer matches", change: func(ref *pb.CommandArtifactRef) { ref.SizeBytes++ }, wantCode: connect.CodeFailedPrecondition, wantError: "no longer matches"},
		{name: "package is not on this host", change: func(ref *pb.CommandArtifactRef) { ref.ArtifactId = uuid.NewString() }, wantCode: connect.CodeNotFound, wantError: "not available on this Fleet host"},
		{name: "ID is not a package ID", change: func(ref *pb.CommandArtifactRef) { ref.ArtifactId = "firmware-1" }, wantCode: connect.CodeInvalidArgument, wantError: "not valid"},
		{name: "no package store", noStore: true, wantCode: connect.CodeInternal, wantError: "not configured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, client := newArtifactTestClient(t)
			store, err := files.NewFrigatePackageStore(t.TempDir())
			require.NoError(t, err)
			if !tc.noStore {
				h.handler.WithControllerSoftwarePackages(store)
			}
			ref := &pb.CommandArtifactRef{
				ArtifactId: storePackage(t, store, payload).String(),
				Purpose:    pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_CONTROLLER_SOFTWARE_PACKAGE,
				Filename:   "frigate_2.0.0_linux_arm64.deb",
				SizeBytes:  int64(len(payload)),
				Sha256:     sha256Hex(payload),
			}
			if tc.change != nil {
				tc.change(ref)
			}
			commandID := "stage-controller-software"
			stream, done := startAckOnlyCommandWithArtifacts(t, h, commandID, []control.ArtifactExpectation{packageDownloadExpectation(ref)})
			download, err := client.DownloadCommandArtifact(context.Background(), connect.NewRequest(&pb.DownloadCommandArtifactRequest{
				CommandId: commandID, Artifact: ref, DeviceIdentifier: "miner-a",
			}))
			require.NoError(t, err)
			defer download.Close()

			if tc.wantError != "" {
				require.False(t, download.Receive())
				require.ErrorContains(t, download.Err(), tc.wantError)
				assert.Equal(t, tc.wantCode, connect.CodeOf(download.Err()))
			} else {
				var got bytes.Buffer
				var header *pb.CommandArtifactRef
				for download.Receive() {
					if h := download.Msg().GetHeader(); h != nil {
						header = h.GetArtifact()
						continue
					}
					got.Write(download.Msg().GetChunk().GetData())
				}
				require.NoError(t, download.Err())
				// The node refuses a header that differs from its command.
				assert.Equal(t, ref.GetArtifactId(), header.GetArtifactId())
				assert.Equal(t, ref.GetPurpose(), header.GetPurpose())
				assert.Equal(t, ref.GetFilename(), header.GetFilename())
				assert.Equal(t, ref.GetSizeBytes(), header.GetSizeBytes())
				assert.Equal(t, ref.GetSha256(), header.GetSha256())
				assert.Equal(t, payload, got.Bytes())
			}
			finishAckOnlyCommand(t, stream, commandID, done)
		})
	}
}

// A command grants one package. The node cannot use it to read another.
func TestDownloadCommandArtifactRefusesAPackageTheCommandDidNotGrant(t *testing.T) {
	h, client := newArtifactTestClient(t)
	store, err := files.NewFrigatePackageStore(t.TempDir())
	require.NoError(t, err)
	h.handler.WithControllerSoftwarePackages(store)
	payload := []byte("package bytes")
	granted := &pb.CommandArtifactRef{
		ArtifactId: storePackage(t, store, payload).String(),
		Purpose:    pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_CONTROLLER_SOFTWARE_PACKAGE,
		Filename:   "frigate.deb", SizeBytes: int64(len(payload)), Sha256: sha256Hex(payload),
	}
	other := &pb.CommandArtifactRef{
		ArtifactId: storePackage(t, store, payload).String(),
		Purpose:    granted.GetPurpose(), Filename: "frigate.deb", SizeBytes: int64(len(payload)), Sha256: sha256Hex(payload),
	}
	commandID := "stage-controller-software"
	stream, done := startAckOnlyCommandWithArtifacts(t, h, commandID, []control.ArtifactExpectation{packageDownloadExpectation(granted)})

	for name, ref := range map[string]*pb.CommandArtifactRef{
		"another package": other,
		"the same ID as firmware": {
			ArtifactId: granted.GetArtifactId(), Purpose: pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_FIRMWARE_PAYLOAD,
			Filename: "frigate.deb", SizeBytes: int64(len(payload)), Sha256: strings.Repeat("0", 64),
		},
	} {
		download, err := client.DownloadCommandArtifact(context.Background(), connect.NewRequest(&pb.DownloadCommandArtifactRequest{
			CommandId: commandID, Artifact: ref, DeviceIdentifier: "miner-a",
		}))
		require.NoError(t, err, name)
		require.False(t, download.Receive(), name)
		require.Error(t, download.Err(), name)
		require.NoError(t, download.Close())
	}
	finishAckOnlyCommand(t, stream, commandID, done)
}
