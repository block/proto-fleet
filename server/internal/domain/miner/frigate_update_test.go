package miner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkpb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
)

const (
	frigateRouteJob         = "0b9f0f4e-1c2d-4e5f-8a6b-7c8d9e0f1a2b"
	frigateRouteFingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// frigateUpdateCalls are the update calls of a handle, by the name of the
// operation that each one sends.
func frigateUpdateCalls(ctx context.Context, m *RemoteFleetNodeMiner) map[string]func() error {
	manifest := sdk.FrigateManifest{
		JobID: frigateRouteJob, InstanceID: "a", ExpectedInstanceIDs: []string{"a", "b"}, TargetVersion: "2.0.0",
		SHA256: strings.Repeat("3", 64), SizeBytes: 4096, Actor: "alice", Coordination: sdk.FrigateCoordinationSelf,
		ReleaseStatement: []byte("statement"), ReleaseSignature: []byte("signature"),
	}
	ref := sdk.FrigateStageRef{JobID: frigateRouteJob, ManifestSHA256: frigateRouteFingerprint}
	action := sdk.FrigateAction{JobID: frigateRouteJob, IdempotencyKey: "key-1"}
	return map[string]func() error{
		"support": func() error { _, err := m.FrigateUpdateSupport(ctx); return err },
		"stage_begin": func() error {
			_, err := m.FrigateStageBegin(ctx, "a", manifest)
			return err
		},
		"stage_append": func() error {
			_, err := m.FrigateStageAppend(ctx, "a", sdk.FrigateStageAppend{JobID: frigateRouteJob, ManifestSHA256: frigateRouteFingerprint, Chunk: []byte{1}})
			return err
		},
		"stage_commit": func() error { _, err := m.FrigateStageCommit(ctx, "a", ref); return err },
		"stage_abort":  func() error { return m.FrigateStageAbort(ctx, "a", ref) },
		"start": func() error {
			_, err := m.FrigateUpdateStart(ctx, sdk.FrigateStartRequest{JobID: frigateRouteJob, TargetVersion: "2.0.0", ExpectedInstanceIDs: []string{"a", "b"}, IdempotencyKey: "key-1"})
			return err
		},
		"status": func() error { _, err := m.FrigateUpdateStatus(ctx, frigateRouteJob); return err },
		"retry":  func() error { _, err := m.FrigateUpdateRetry(ctx, action); return err },
		"cancel": func() error { _, err := m.FrigateUpdateCancel(ctx, action); return err },
		"stage": func() error {
			_, err := m.FrigateStagePackage(ctx, interfaces.FrigatePackageRef{}, []interfaces.FrigateStageTarget{{InstanceID: "a"}})
			return err
		},
	}
}

// A handle with no command miner, such as a telemetry handle, cannot reach the
// controllers for an update.
func TestRemoteFleetNodeMinerWithoutACommandMinerRefusesFrigateUpdates(t *testing.T) {
	for name, call := range frigateUpdateCalls(t.Context(), &RemoteFleetNodeMiner{}) {
		err := call()
		require.Error(t, err, name)
		require.True(t, fleeterror.IsFailedPreconditionError(err), name)
		require.Contains(t, err.Error(), "Fleet Node", name)
	}
}

var errSentToNode = errors.New("sent to the fleet node")

// commandMiner records the update calls that reach the Fleet Node path.
type commandMiner struct {
	interfaces.Miner
	sent   []string
	staged bool
}

func (c *commandMiner) FrigateRelay() sdk.FrigateRelay {
	return sdk.NewFrigateRelay(func(_ context.Context, request []byte) ([]byte, error) {
		var req sdkpb.FrigateServerUpdateRequest
		if err := proto.Unmarshal(request, &req); err != nil {
			return nil, fmt.Errorf("decode relayed request: %w", err)
		}
		msg := req.ProtoReflect()
		c.sent = append(c.sent, string(msg.WhichOneof(msg.Descriptor().Oneofs().ByName("operation")).Name()))
		return nil, errSentToNode
	})
}

func (c *commandMiner) FrigateStagePackage(context.Context, interfaces.FrigatePackageRef, []interfaces.FrigateStageTarget) ([]interfaces.FrigateStageOutcome, error) {
	c.staged = true
	return nil, nil
}

func TestRemoteFleetNodeMinerSendsFrigateUpdatesThroughItsCommandMiner(t *testing.T) {
	delegate := &commandMiner{}
	for name, call := range frigateUpdateCalls(t.Context(), &RemoteFleetNodeMiner{delegate: delegate}) {
		delegate.sent = nil
		err := call()
		switch name {
		case "stage":
			require.NoError(t, err, name)
			require.True(t, delegate.staged)
		case "stage_append":
			require.ErrorIs(t, err, sdk.ErrFrigateRelayAppend, "package bytes do not cross the control stream")
			require.Empty(t, delegate.sent)
		default:
			require.ErrorIs(t, err, errSentToNode, name)
			require.Equal(t, []string{name}, delegate.sent)
		}
	}
}
