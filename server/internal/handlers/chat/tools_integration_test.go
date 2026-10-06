package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	collectionv1 "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	devicesetv1 "github.com/block/proto-fleet/server/generated/grpc/device_set/v1"
	"github.com/block/proto-fleet/server/internal/domain/collection"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/handlers/deviceset"
	"github.com/block/proto-fleet/server/internal/testutil"
)

type failingRackSlotHandler struct {
	deviceSetsHandler
	failSet bool
	calls   int
}

func (h *failingRackSlotHandler) SetRackSlotPosition(ctx context.Context, req *connect.Request[devicesetv1.SetRackSlotPositionRequest]) (*connect.Response[devicesetv1.SetRackSlotPositionResponse], error) {
	if h.failSet {
		h.calls++
		if h.calls == 2 {
			return nil, errors.New("late slot assignment failure")
		}
	}
	return h.deviceSetsHandler.SetRackSlotPosition(ctx, req)
}

func (h *failingRackSlotHandler) ClearRackSlotPosition(ctx context.Context, req *connect.Request[devicesetv1.ClearRackSlotPositionRequest]) (*connect.Response[devicesetv1.ClearRackSlotPositionResponse], error) {
	if !h.failSet {
		h.calls++
		if h.calls == 2 {
			return nil, errors.New("late slot clearing failure")
		}
	}
	return h.deviceSetsHandler.ClearRackSlotPosition(ctx, req)
}

func TestRackSlotToolsRollBackEarlierMutationsOnLateFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	for _, operation := range []string{"set_rack_slots", "clear_rack_slots"} {
		t.Run(operation, func(t *testing.T) {
			infra := testutil.InitializeDBServiceInfrastructure(t)
			admin := infra.DatabaseService.CreateSuperAdminUser()
			ctx := testutil.MockAuthContextForTesting(t.Context(), admin.DatabaseID, admin.OrganizationID)
			store := sqlstores.NewSQLCollectionStore(infra.DatabaseService.DB)
			transactor := sqlstores.NewSQLTransactor(infra.DatabaseService.DB)
			rack, err := store.CreateCollection(ctx, admin.OrganizationID, collectionv1.CollectionType_COLLECTION_TYPE_RACK, "Rollback rack", "")
			require.NoError(t, err)
			require.NoError(t, store.CreateRackExtension(ctx, interfaces.CreateRackExtensionParams{
				OrgID: admin.OrganizationID, CollectionID: rack.Id, Rows: 2, Columns: 2, OrderIndex: 2,
			}))
			deviceIDs := []string{
				infra.DatabaseService.CreateDevice(admin.OrganizationID, "proto").ID,
				infra.DatabaseService.CreateDevice(admin.OrganizationID, "proto").ID,
			}
			_, err = store.AddDevicesToCollection(ctx, admin.OrganizationID, rack.Id, deviceIDs)
			require.NoError(t, err)
			column := int32(0)
			for _, identifier := range deviceIDs {
				require.NoError(t, store.SetRackSlotPosition(ctx, rack.Id, identifier, 0, column, admin.OrganizationID))
				column++
			}
			before, err := store.GetRackSlots(ctx, rack.Id, admin.OrganizationID)
			require.NoError(t, err)
			service := collection.NewService(store, nil, nil, nil, transactor, nil, nil, nil)
			handler := &failingRackSlotHandler{deviceSetsHandler: deviceset.NewHandler(service), failSet: operation == "set_rack_slots"}
			tools := NewFleetTools(nil, nil, nil, handler, nil, nil, transactor)
			arguments := fmt.Sprintf(`{"rack_id":%d,"device_identifiers":[%q,%q]}`, rack.Id, deviceIDs[0], deviceIDs[1])
			if operation == "set_rack_slots" {
				arguments = fmt.Sprintf(`{"rack_id":%d,"slot_assignments":[{"device_identifier":%q,"row":0,"column":1},{"device_identifier":%q,"row":0,"column":0}]}`, rack.Id, deviceIDs[0], deviceIDs[1])
			}
			_, err = tools.Execute(ctx, operation, json.RawMessage(arguments))
			require.ErrorContains(t, err, "late slot")
			after, err := store.GetRackSlots(ctx, rack.Id, admin.OrganizationID)
			require.NoError(t, err)
			assert.ElementsMatch(t, before, after, "failed tool must preserve every original slot")

			tools = NewFleetTools(nil, nil, nil, handler.deviceSetsHandler, nil, nil, transactor)
			_, err = tools.Execute(ctx, operation, json.RawMessage(arguments))
			require.NoError(t, err)
			after, err = store.GetRackSlots(ctx, rack.Id, admin.OrganizationID)
			require.NoError(t, err)
			if operation == "clear_rack_slots" {
				assert.Empty(t, after, "successful clear must commit")
			} else {
				require.Len(t, after, 2)
				positions := make(map[string]int32, len(after))
				for _, slot := range after {
					assert.Zero(t, slot.GetPosition().GetRow())
					positions[slot.GetDeviceIdentifier()] = slot.GetPosition().GetColumn()
				}
				assert.Equal(t, map[string]int32{deviceIDs[0]: 1, deviceIDs[1]: 0}, positions, "successful swap must commit")
			}
		})
	}
}
