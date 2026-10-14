package command

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/generated/sqlc"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	minerIfaceMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type fencedCurtailmentConfigMiner struct {
	*minerIfaceMocks.MockMiner
	configure func(context.Context, dto.ApplyCurtailmentConfigPayload) error
}

func (m *fencedCurtailmentConfigMiner) ApplyCurtailmentConfig(ctx context.Context, payload dto.ApplyCurtailmentConfigPayload) error {
	return m.configure(ctx, payload)
}

func TestQueuedCurtailmentWorkerDispatchesAcrossOwnershipHandoff(t *testing.T) {
	for _, kind := range []commandtype.Type{commandtype.Curtail, commandtype.Uncurtail, commandtype.ApplyCurtailmentConfig} {
		t.Run(kind.String(), func(t *testing.T) {
			_, conn, _, deviceID, _ := setupPasswordCommandDevice(t, "proto", false)
			q := sqlc.New(conn)
			actor, err := q.CreateUser(t.Context(), sqlc.CreateUserParams{UserID: id.GenerateID(), Username: "curtailment-worker", PasswordHash: "unused", CreatedAt: time.Now()})
			require.NoError(t, err)
			ctx := authn.SetInfo(t.Context(), &session.Info{UserID: actor, OrganizationID: 1, Actor: session.ActorCurtailment})
			batch := id.GenerateID()
			_, err = q.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: kind.String(), CreatedBy: actor, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 1, OrganizationID: sql.NullInt64{Int64: 1, Valid: true}})
			require.NoError(t, err)
			payload := []byte(`{}`)
			if kind == commandtype.Curtail {
				payload, err = json.Marshal(dto.CurtailPayload{Level: int32(sdk.CurtailLevelFull)})
			} else if kind == commandtype.ApplyCurtailmentConfig {
				payload, err = json.Marshal(curtailmentConfigQueuePayload{FleetNodeEncryptedConfig: &dto.NodeEncryptedPayload{Algorithm: "encrypted-fixture", Ciphertext: []byte("config")}})
			}
			require.NoError(t, err)
			messages := queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 3}, conn)
			require.NoError(t, messages.Enqueue(ctx, batch, kind, []int64{deviceID}, json.RawMessage(payload)))
			fences := devicewrite.NewService(conn)
			_, err = fences.Reserve(ctx, 1, deviceID)
			require.NoError(t, err)
			rows, err := messages.Dequeue(ctx, 1)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			ctrl := gomock.NewController(t)
			getter := minerMocks.NewMockCachedMinerGetter(ctrl)
			mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
			dispatches := 0
			dispatch := func(ctx context.Context) error {
				return fences.WithCurtailmentAttempt(ctx, 1, deviceID, 0, kind, func(ctx context.Context) error {
					return devicewrite.Dispatch(ctx, func(ctx context.Context) error {
						dispatches++
						_, err := conn.ExecContext(ctx, `UPDATE device_write_fence SET state='active',epoch=epoch+1 WHERE device_id=$1`, deviceID)
						if err != nil {
							return fmt.Errorf("advance test ownership: %w", err)
						}
						return devicewrite.RecheckContext(ctx)
					})
				})
			}
			handle := &fencedCurtailmentConfigMiner{MockMiner: mockMiner, configure: func(ctx context.Context, p dto.ApplyCurtailmentConfigPayload) error {
				require.NotNil(t, p.EncryptedConfig)
				return dispatch(ctx)
			}}
			getter.EXPECT().GetMinerForCommand(gomock.Any(), deviceID).Return(handle, nil)
			mockMiner.EXPECT().GetOrgID().Return(int64(1))
			mockMiner.EXPECT().GetSiteID().Return(int64(0))
			if kind == commandtype.Curtail {
				mockMiner.EXPECT().Curtail(gomock.Any(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}).DoAndReturn(func(ctx context.Context, _ sdk.CurtailRequest) error { return dispatch(ctx) })
			} else if kind == commandtype.Uncurtail {
				mockMiner.EXPECT().Uncurtail(gomock.Any(), sdk.UncurtailRequest{}).DoAndReturn(func(ctx context.Context, _ sdk.UncurtailRequest) error { return dispatch(ctx) })
			}
			worker := NewExecutionService(&Config{MaxWorkers: 1}, conn, messages, nil, nil, getter, nil, nil, nil)
			_, _, err = worker.executeQueuedCommand(ctx, rows[0])
			require.NoError(t, err)
			require.Equal(t, 1, dispatches)
			var scope, state string
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT curtailment_command,status FROM device_write_attempt WHERE queue_message_id=$1`, rows[0].ID).Scan(&scope, &state))
			require.Equal(t, kind.String(), scope)
			require.Equal(t, "complete", state, "returned fleet controls use the ordered completion policy")
		})
	}
}
