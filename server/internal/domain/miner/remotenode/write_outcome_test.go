package remotenode

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func TestMiner_PreSendFailuresAreNotDispatched(t *testing.T) {
	poolPayload := dto.UpdateMiningPoolsPayload{DefaultPool: dto.MiningPool{URL: "stratum+tcp://pool.example.com:3333", Username: "worker"}}
	firmware := sdk.FirmwareFile{ID: "artifact", Filename: "update.swu", Size: 1, SHA256: strings.Repeat("a", 64)}
	for _, tc := range []struct {
		name      string
		operation func(*Miner) error
	}{
		{"pool validation", func(m *Miner) error { return m.UpdateMiningPools(t.Context(), dto.UpdateMiningPoolsPayload{}) }},
		{"missing key provider", func(m *Miner) error {
			m.encryptionPublicKey = nil
			return m.UpdateMiningPools(t.Context(), poolPayload)
		}},
		{"key lookup", func(m *Miner) error {
			m.encryptionPublicKey = func(context.Context) ([]byte, error) { return nil, errors.New("key unavailable") }
			return m.UpdateMiningPools(t.Context(), poolPayload)
		}},
		{"encryption sealing", func(m *Miner) error {
			m.encryptionPublicKey = func(context.Context) ([]byte, error) { return []byte("invalid key"), nil }
			return m.UpdateMiningPools(t.Context(), poolPayload)
		}},
		{"command validation", func(m *Miner) error {
			_, err := m.sendWithoutGate(t.Context(), &gatewaypb.MinerCommand{})
			return err
		}},
		{"artifact command validation", func(m *Miner) error {
			_, _, err := m.sendWithoutGateWithArtifactResults(t.Context(), &gatewaypb.MinerCommand{}, nil)
			return err
		}},
		{"missing artifact sender", func(m *Miner) error { m.sender = &blockingSender{}; return m.FirmwareUpdate(t.Context(), firmware) }},
		{"missing curtailment payload", func(m *Miner) error {
			return m.ApplyCurtailmentConfig(t.Context(), dto.ApplyCurtailmentConfigPayload{})
		}},
		{"missing password payload", func(m *Miner) error { return m.UpdateMinerPassword(t.Context(), dto.UpdateMinerPasswordPayload{}) }},
		{"missing firmware ID", func(m *Miner) error { f := firmware; f.ID = ""; return m.FirmwareUpdate(t.Context(), f) }},
		{"missing firmware filename", func(m *Miner) error { f := firmware; f.Filename = ""; return m.FirmwareUpdate(t.Context(), f) }},
		{"missing firmware size", func(m *Miner) error { f := firmware; f.Size = 0; return m.FirmwareUpdate(t.Context(), f) }},
		{"missing firmware hash", func(m *Miner) error { f := firmware; f.SHA256 = ""; return m.FirmwareUpdate(t.Context(), f) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := okSender()
			err := tc.operation(newTestMiner(t, sender))
			require.Error(t, err)
			require.True(t, devicewrite.IsNotDispatched(err))
			require.Nil(t, sender.cmd, "no command may reach transport for a proven preparation failure")
		})
	}
}

func TestMiner_PostSendFailuresRemainUncertain(t *testing.T) {
	firmware := sdk.FirmwareFile{ID: "artifact", Filename: "update.swu", Size: 1, SHA256: strings.Repeat("a", 64)}
	for _, operation := range []struct {
		name string
		run  func(*Miner) error
	}{
		{"ordinary command", func(m *Miner) error { return m.StopMining(t.Context()) }},
		{"artifact command", func(m *Miner) error { return m.FirmwareUpdate(t.Context(), firmware) }},
	} {
		for _, failure := range []struct {
			name   string
			sender *fakeSender
		}{
			{"transport", &fakeSender{err: context.DeadlineExceeded}},
			{"acknowledgement", &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_INTERNAL}}},
		} {
			t.Run(operation.name+"/"+failure.name, func(t *testing.T) {
				err := operation.run(newTestMiner(t, failure.sender))
				require.Error(t, err)
				require.False(t, devicewrite.IsNotDispatched(err))
				require.NotNil(t, failure.sender.cmd, "transport was invoked, so the physical outcome remains uncertain")
			})
		}
	}
}

func TestMiner_BusyAcknowledgementIsNotDispatchedOnlyWhenRejected(t *testing.T) {
	for _, succeeded := range []bool{false, true} {
		t.Run(fmt.Sprintf("succeeded=%t", succeeded), func(t *testing.T) {
			sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY, Succeeded: succeeded}}
			err := newTestMiner(t, sender).StopMining(t.Context())
			require.Error(t, err)
			require.Equal(t, !succeeded, devicewrite.IsNotDispatched(err))
			require.False(t, fleeterror.IsFailedPreconditionError(err), "BUSY remains retryable")
			require.NotNil(t, sender.cmd)
		})
	}
}
