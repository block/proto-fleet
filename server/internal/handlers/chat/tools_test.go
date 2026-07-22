package chat

import (
	"context"
	"encoding/json"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	devicesetv1 "github.com/block/proto-fleet/server/generated/grpc/device_set/v1"
	fleetv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	poolsv1 "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	sitesv1 "github.com/block/proto-fleet/server/generated/grpc/sites/v1"
	chatdomain "github.com/block/proto-fleet/server/internal/domain/chat"
)

type staticFleetHandler struct {
	miners       []*fleetv1.MinerStateSnapshot
	totalMiners  int32
	cursor       string
	listRequests []*fleetv1.ListMinerStateSnapshotsRequest
}

func (h *staticFleetHandler) GetMinerStateCounts(context.Context, *connect.Request[fleetv1.GetMinerStateCountsRequest]) (*connect.Response[fleetv1.GetMinerStateCountsResponse], error) {
	return connect.NewResponse(&fleetv1.GetMinerStateCountsResponse{}), nil
}

func (h *staticFleetHandler) ListMinerStateSnapshots(_ context.Context, request *connect.Request[fleetv1.ListMinerStateSnapshotsRequest]) (*connect.Response[fleetv1.ListMinerStateSnapshotsResponse], error) {
	h.listRequests = append(h.listRequests, request.Msg)
	return connect.NewResponse(&fleetv1.ListMinerStateSnapshotsResponse{
		Miners:      h.miners,
		TotalMiners: h.totalMiners,
		Cursor:      h.cursor,
	}), nil
}

type staticPoolsHandler struct {
	pools []*poolsv1.Pool
}

type recordingSitesHandler struct {
	createRequest *sitesv1.CreateSiteRequest
}

func (*recordingSitesHandler) ListSites(context.Context, *connect.Request[sitesv1.ListSitesRequest]) (*connect.Response[sitesv1.ListSitesResponse], error) {
	return connect.NewResponse(&sitesv1.ListSitesResponse{}), nil
}

func (h *recordingSitesHandler) CreateSite(_ context.Context, request *connect.Request[sitesv1.CreateSiteRequest]) (*connect.Response[sitesv1.CreateSiteResponse], error) {
	h.createRequest = request.Msg
	return connect.NewResponse(&sitesv1.CreateSiteResponse{Site: &sitesv1.Site{Id: 12, Name: request.Msg.GetName()}}), nil
}

type recordingDeviceSetsHandler struct {
	createRequest *devicesetv1.CreateDeviceSetRequest
	moveRequest   *devicesetv1.AssignDevicesToRackRequest
	setRequests   []*devicesetv1.SetRackSlotPositionRequest
	clearRequests []*devicesetv1.ClearRackSlotPositionRequest
	rack          *devicesetv1.DeviceSet
	members       []*devicesetv1.DeviceSetMember
	slots         []*devicesetv1.RackSlot
}

func (h *recordingDeviceSetsHandler) GetDeviceSet(context.Context, *connect.Request[devicesetv1.GetDeviceSetRequest]) (*connect.Response[devicesetv1.GetDeviceSetResponse], error) {
	rack := h.rack
	if rack == nil {
		rack = &devicesetv1.DeviceSet{
			Id:    21,
			Type:  devicesetv1.DeviceSetType_DEVICE_SET_TYPE_RACK,
			Label: "A1",
			TypeDetails: &devicesetv1.DeviceSet_RackInfo{RackInfo: &devicesetv1.RackInfo{
				Rows:        4,
				Columns:     6,
				OrderIndex:  devicesetv1.RackOrderIndex_RACK_ORDER_INDEX_TOP_LEFT,
				CoolingType: devicesetv1.RackCoolingType_RACK_COOLING_TYPE_AIR,
			}},
		}
	}
	return connect.NewResponse(&devicesetv1.GetDeviceSetResponse{DeviceSet: rack}), nil
}

func (*recordingDeviceSetsHandler) ListDeviceSets(context.Context, *connect.Request[devicesetv1.ListDeviceSetsRequest]) (*connect.Response[devicesetv1.ListDeviceSetsResponse], error) {
	return connect.NewResponse(&devicesetv1.ListDeviceSetsResponse{}), nil
}

func (h *recordingDeviceSetsHandler) ListDeviceSetMembers(context.Context, *connect.Request[devicesetv1.ListDeviceSetMembersRequest]) (*connect.Response[devicesetv1.ListDeviceSetMembersResponse], error) {
	members := h.members
	if members == nil {
		members = []*devicesetv1.DeviceSetMember{
			{DeviceIdentifier: "miner-a"},
			{DeviceIdentifier: "miner-b"},
		}
	}
	return connect.NewResponse(&devicesetv1.ListDeviceSetMembersResponse{Members: members}), nil
}

func (h *recordingDeviceSetsHandler) CreateDeviceSet(_ context.Context, request *connect.Request[devicesetv1.CreateDeviceSetRequest]) (*connect.Response[devicesetv1.CreateDeviceSetResponse], error) {
	h.createRequest = request.Msg
	return connect.NewResponse(&devicesetv1.CreateDeviceSetResponse{DeviceSet: &devicesetv1.DeviceSet{Id: 21, Label: request.Msg.GetLabel()}}), nil
}

func (h *recordingDeviceSetsHandler) AssignDevicesToRack(_ context.Context, request *connect.Request[devicesetv1.AssignDevicesToRackRequest]) (*connect.Response[devicesetv1.AssignDevicesToRackResponse], error) {
	h.moveRequest = request.Msg
	return connect.NewResponse(&devicesetv1.AssignDevicesToRackResponse{AssignedCount: 2, RemovedCount: 1}), nil
}

func (h *recordingDeviceSetsHandler) SetRackSlotPosition(_ context.Context, request *connect.Request[devicesetv1.SetRackSlotPositionRequest]) (*connect.Response[devicesetv1.SetRackSlotPositionResponse], error) {
	h.setRequests = append(h.setRequests, request.Msg)
	return connect.NewResponse(&devicesetv1.SetRackSlotPositionResponse{
		DeviceSetId: request.Msg.GetDeviceSetId(),
		Slot: &devicesetv1.RackSlot{
			DeviceIdentifier: request.Msg.GetDeviceIdentifier(),
			Position:         request.Msg.GetPosition(),
		},
	}), nil
}

func (h *recordingDeviceSetsHandler) ClearRackSlotPosition(_ context.Context, request *connect.Request[devicesetv1.ClearRackSlotPositionRequest]) (*connect.Response[devicesetv1.ClearRackSlotPositionResponse], error) {
	h.clearRequests = append(h.clearRequests, request.Msg)
	return connect.NewResponse(&devicesetv1.ClearRackSlotPositionResponse{}), nil
}

func (h *recordingDeviceSetsHandler) GetRackSlots(context.Context, *connect.Request[devicesetv1.GetRackSlotsRequest]) (*connect.Response[devicesetv1.GetRackSlotsResponse], error) {
	return connect.NewResponse(&devicesetv1.GetRackSlotsResponse{Slots: h.slots}), nil
}

func (h staticPoolsHandler) ListPools(context.Context, *connect.Request[poolsv1.ListPoolsRequest]) (*connect.Response[poolsv1.ListPoolsResponse], error) {
	return connect.NewResponse(&poolsv1.ListPoolsResponse{Pools: h.pools}), nil
}

func TestListPoolsOnlyDisclosesNamesToModelProvider(t *testing.T) {
	tools := NewFleetTools(nil, nil, staticPoolsHandler{pools: []*poolsv1.Pool{{
		PoolId:   42,
		PoolName: "Primary pool",
		Url:      "stratum+tcp://pool.example.com:3333",
		Username: "bc1q-wallet.worker-01",
	}}}, nil)

	output, err := tools.Execute(t.Context(), "list_pools", json.RawMessage(`{}`))

	require.NoError(t, err)
	assert.JSONEq(t, `{"pools":[{"name":"Primary pool"}]}`, output.Content)
	assert.NotContains(t, output.Content, "pool.example.com")
	assert.NotContains(t, output.Content, "bc1q-wallet")
	assert.NotContains(t, output.Content, "42")
}

func TestWriteToolDefinitionsRequireConfirmation(t *testing.T) {
	tools := NewFleetTools(nil, &recordingSitesHandler{}, nil, &recordingDeviceSetsHandler{})
	requiresConfirmation := make(map[string]bool)
	for _, definition := range tools.Definitions() {
		requiresConfirmation[definition.Name] = definition.RequiresConfirmation
	}

	assert.True(t, requiresConfirmation["create_site"])
	assert.True(t, requiresConfirmation["create_rack"])
	assert.True(t, requiresConfirmation["move_miners_to_rack"])
	assert.True(t, requiresConfirmation["set_rack_slots"])
	assert.True(t, requiresConfirmation["clear_rack_slots"])
	assert.False(t, requiresConfirmation["resolve_miners"])
	assert.False(t, requiresConfirmation["list_sites"])
	assert.False(t, requiresConfirmation["get_rack_slots"])
}

func TestResolveMinersReturnsExplicitIdentifiersAndPlacement(t *testing.T) {
	fleet := &staticFleetHandler{
		totalMiners: 2,
		miners: []*fleetv1.MinerStateSnapshot{
			{
				DeviceIdentifier: "miner-a",
				Name:             "Alpha 01",
				MacAddress:       "aa:bb:cc:dd:ee:ff",
				SerialNumber:     "SN-ALPHA",
				IpAddress:        "10.0.0.10",
				DeviceStatus:     fleetv1.DeviceStatus_DEVICE_STATUS_OFFLINE,
				Model:            "S21",
				Placement: &commonv1.PlacementRefs{
					Site:     &commonv1.ResourceRef{Id: 9, Label: "North"},
					Building: &commonv1.ResourceRef{Id: 3, Label: "Building A"},
					Rack:     &commonv1.ResourceRef{Id: 21, Label: "A1"},
					Zone:     "Hot aisle",
				},
			},
			{
				DeviceIdentifier: "miner-b",
				Name:             "Beta 01",
				DeviceStatus:     fleetv1.DeviceStatus_DEVICE_STATUS_ONLINE,
			},
		},
	}
	tools := NewFleetTools(fleet, nil, nil, nil)

	output, err := tools.Execute(t.Context(), "resolve_miners", json.RawMessage(`{
		"query":"alpha",
		"device_statuses":["offline"],
		"site_ids":[9],
		"include_no_rack":true,
		"limit":1
	}`))

	require.NoError(t, err)
	require.Len(t, fleet.listRequests, 1)
	request := fleet.listRequests[0]
	assert.Equal(t, int32(1000), request.GetPageSize(), "query resolution scans a full page")
	assert.Equal(t, []fleetv1.DeviceStatus{fleetv1.DeviceStatus_DEVICE_STATUS_OFFLINE}, request.GetFilter().GetDeviceStatus())
	assert.Equal(t, []int64{9}, request.GetFilter().GetSiteIds())
	assert.True(t, request.GetFilter().GetIncludeNoRack())
	assert.JSONEq(t, `{
		"device_identifiers":["miner-a"],
		"miners":[{
			"device_identifier":"miner-a",
			"name":"Alpha 01",
			"status":"offline",
			"model":"S21",
			"ip_address":"10.0.0.10",
			"site":{"id":9,"label":"North"},
			"building":{"id":3,"label":"Building A"},
			"rack":{"id":21,"label":"A1"},
			"zone":"Hot aisle"
		}],
		"returned":1,
		"matched_scanned":1,
		"total_available":2,
		"truncated":false,
		"query":"alpha"
	}`, output.Content)
	assert.NotContains(t, output.Content, "aa:bb:cc")
	assert.NotContains(t, output.Content, "SN-ALPHA")
	assert.Equal(t, "Resolved 1 miner(s)", output.Summary)
}

func TestResolveMinersReportsTruncatedMatches(t *testing.T) {
	fleet := &staticFleetHandler{
		totalMiners: 2,
		miners: []*fleetv1.MinerStateSnapshot{
			{DeviceIdentifier: "miner-a", DeviceStatus: fleetv1.DeviceStatus_DEVICE_STATUS_OFFLINE},
			{DeviceIdentifier: "miner-b", DeviceStatus: fleetv1.DeviceStatus_DEVICE_STATUS_OFFLINE},
		},
		cursor: "next-page",
	}
	tools := NewFleetTools(fleet, nil, nil, nil)

	output, err := tools.Execute(t.Context(), "resolve_miners", json.RawMessage(`{"device_statuses":["broken"],"limit":1}`))

	require.NoError(t, err)
	assert.Contains(t, output.Summary, "more matches may exist")
	assert.JSONEq(t, `{
		"device_identifiers":["miner-a"],
		"miners":[{"device_identifier":"miner-a","status":"offline"}],
		"returned":1,
		"matched_scanned":2,
		"total_available":2,
		"truncated":true
	}`, output.Content)
	require.Len(t, fleet.listRequests, 1)
	assert.Equal(t, []fleetv1.DeviceStatus{fleetv1.DeviceStatus_DEVICE_STATUS_ERROR}, fleet.listRequests[0].GetFilter().GetDeviceStatus())
}

func TestResolveMinersRejectsInvalidStatus(t *testing.T) {
	tools := NewFleetTools(&staticFleetHandler{}, nil, nil, nil)

	_, err := tools.Execute(t.Context(), "resolve_miners", json.RawMessage(`{"device_statuses":["reticulating"]}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `unsupported device_status "reticulating"`)
}

func TestCreateSiteConfirmationMatchesExecutedRequest(t *testing.T) {
	sites := &recordingSitesHandler{}
	tools := NewFleetTools(nil, sites, nil, nil)
	arguments := json.RawMessage(`{"name":"North","location_city":"Austin","location_state":"TX","country":"US","power_capacity_mw":12.5}`)

	confirmation, err := tools.Confirmation("create_site", arguments)
	require.NoError(t, err)
	require.NotNil(t, confirmation)
	assert.Equal(t, "Create this site?", confirmation.Title)
	assert.Contains(t, confirmation.Details, chatdomain.ToolConfirmationDetail{Label: "Name", Value: "North"})
	assert.Contains(t, confirmation.Details, chatdomain.ToolConfirmationDetail{Label: "Power capacity", Value: "12.5 MW"})

	output, err := tools.Execute(t.Context(), "create_site", arguments)
	require.NoError(t, err)
	require.NotNil(t, sites.createRequest)
	assert.Equal(t, "North", sites.createRequest.GetName())
	assert.Equal(t, 12.5, sites.createRequest.GetPowerCapacityMw())
	assert.JSONEq(t, `{"created":true,"site_id":12,"name":"North","warnings":[]}`, output.Content)
}

func TestCreateRackAndMoveMinersUseValidatedExplicitInputs(t *testing.T) {
	deviceSets := &recordingDeviceSetsHandler{}
	tools := NewFleetTools(nil, nil, nil, deviceSets)

	confirmation, err := tools.Confirmation("create_rack", json.RawMessage(`{"label":"A1","rows":4,"columns":6,"site_id":9}`))
	require.NoError(t, err)
	assert.Contains(t, confirmation.Details, chatdomain.ToolConfirmationDetail{Label: "Layout", Value: "4 rows × 6 columns"})
	_, err = tools.Execute(t.Context(), "create_rack", json.RawMessage(`{"label":"A1","rows":4,"columns":6,"site_id":9}`))
	require.NoError(t, err)
	require.NotNil(t, deviceSets.createRequest)
	assert.Equal(t, int64(9), deviceSets.createRequest.GetRackInfo().GetSiteId())

	moveArguments := json.RawMessage(`{"target_rack_id":21,"device_identifiers":["miner-a","miner-b"]}`)
	_, err = tools.Execute(t.Context(), "move_miners_to_rack", moveArguments)
	require.NoError(t, err)
	require.NotNil(t, deviceSets.moveRequest)
	assert.Equal(t, int64(21), deviceSets.moveRequest.GetTargetRackId())
	assert.Equal(t, []string{"miner-a", "miner-b"}, deviceSets.moveRequest.GetDeviceSelector().GetDeviceList().GetDeviceIdentifiers())
}

func TestGetRackSlotsReturnsOccupiedSlots(t *testing.T) {
	deviceSets := &recordingDeviceSetsHandler{
		slots: []*devicesetv1.RackSlot{
			{DeviceIdentifier: "miner-a", Position: &devicesetv1.RackSlotPosition{Row: 0, Column: 1}},
			{DeviceIdentifier: "miner-b", Position: &devicesetv1.RackSlotPosition{Row: 1, Column: 0}},
		},
	}
	tools := NewFleetTools(nil, nil, nil, deviceSets)

	output, err := tools.Execute(t.Context(), "get_rack_slots", json.RawMessage(`{"rack_id":21}`))

	require.NoError(t, err)
	assert.JSONEq(t, `{
		"rack_id":21,
		"occupied_count":2,
		"occupied_slots":[
			{"device_identifier":"miner-a","row":0,"column":1},
			{"device_identifier":"miner-b","row":1,"column":0}
		]
	}`, output.Content)
	assert.Equal(t, "Read 2 occupied slot(s) for rack 21", output.Summary)
}

func TestSetRackSlotsClearsThenAssignsRequestedSlots(t *testing.T) {
	deviceSets := &recordingDeviceSetsHandler{
		members: []*devicesetv1.DeviceSetMember{
			{DeviceIdentifier: "miner-a"},
			{DeviceIdentifier: "miner-b"},
			{DeviceIdentifier: "miner-c"},
		},
		slots: []*devicesetv1.RackSlot{
			{DeviceIdentifier: "miner-a", Position: &devicesetv1.RackSlotPosition{Row: 0, Column: 0}},
			{DeviceIdentifier: "miner-c", Position: &devicesetv1.RackSlotPosition{Row: 2, Column: 0}},
		},
	}
	tools := NewFleetTools(nil, nil, nil, deviceSets)
	arguments := json.RawMessage(`{"rack_id":21,"slot_assignments":[
		{"device_identifier":"miner-a","row":0,"column":1},
		{"device_identifier":"miner-b","row":0,"column":0}
	]}`)

	confirmation, err := tools.Confirmation("set_rack_slots", arguments)
	require.NoError(t, err)
	assert.Equal(t, "Assign 2 rack slot(s)?", confirmation.Title)
	assert.Contains(t, confirmation.Details, chatdomain.ToolConfirmationDetail{Label: "Rack ID", Value: "21"})

	output, err := tools.Execute(t.Context(), "set_rack_slots", arguments)

	require.NoError(t, err)
	require.Len(t, deviceSets.clearRequests, 2)
	assert.ElementsMatch(t, []string{"miner-a", "miner-b"}, []string{
		deviceSets.clearRequests[0].GetDeviceIdentifier(),
		deviceSets.clearRequests[1].GetDeviceIdentifier(),
	})
	require.Len(t, deviceSets.setRequests, 2)
	assert.Equal(t, int64(21), deviceSets.setRequests[0].GetDeviceSetId())
	assert.Equal(t, "miner-a", deviceSets.setRequests[0].GetDeviceIdentifier())
	assert.Equal(t, int32(0), deviceSets.setRequests[0].GetPosition().GetRow())
	assert.Equal(t, int32(1), deviceSets.setRequests[0].GetPosition().GetColumn())
	assert.Equal(t, "miner-b", deviceSets.setRequests[1].GetDeviceIdentifier())
	assert.Equal(t, int32(0), deviceSets.setRequests[1].GetPosition().GetRow())
	assert.Equal(t, int32(0), deviceSets.setRequests[1].GetPosition().GetColumn())
	assert.JSONEq(t, `{
		"applied":true,
		"rack_id":21,
		"rack_label":"A1",
		"assigned_count":2,
		"slot_assignments":[
			{"device_identifier":"miner-a","row":0,"column":1},
			{"device_identifier":"miner-b","row":0,"column":0}
		]
	}`, output.Content)
}

func TestSetRackSlotsRejectsUnrequestedOccupiedSlot(t *testing.T) {
	deviceSets := &recordingDeviceSetsHandler{
		members: []*devicesetv1.DeviceSetMember{
			{DeviceIdentifier: "miner-a"},
			{DeviceIdentifier: "miner-b"},
		},
		slots: []*devicesetv1.RackSlot{
			{DeviceIdentifier: "miner-b", Position: &devicesetv1.RackSlotPosition{Row: 0, Column: 1}},
		},
	}
	tools := NewFleetTools(nil, nil, nil, deviceSets)

	_, err := tools.Execute(t.Context(), "set_rack_slots", json.RawMessage(`{"rack_id":21,"slot_assignments":[
		{"device_identifier":"miner-a","row":0,"column":1}
	]}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `slot (0,1) is already occupied by miner "miner-b"`)
	assert.Empty(t, deviceSets.clearRequests)
	assert.Empty(t, deviceSets.setRequests)
}

func TestClearRackSlotsClearsOnlyRequestedMiners(t *testing.T) {
	deviceSets := &recordingDeviceSetsHandler{
		members: []*devicesetv1.DeviceSetMember{
			{DeviceIdentifier: "miner-a"},
			{DeviceIdentifier: "miner-b"},
		},
		slots: []*devicesetv1.RackSlot{
			{DeviceIdentifier: "miner-a", Position: &devicesetv1.RackSlotPosition{Row: 0, Column: 1}},
			{DeviceIdentifier: "miner-b", Position: &devicesetv1.RackSlotPosition{Row: 1, Column: 1}},
		},
	}
	tools := NewFleetTools(nil, nil, nil, deviceSets)

	output, err := tools.Execute(t.Context(), "clear_rack_slots", json.RawMessage(`{"rack_id":21,"device_identifiers":["miner-a"]}`))

	require.NoError(t, err)
	require.Len(t, deviceSets.clearRequests, 1)
	assert.Equal(t, int64(21), deviceSets.clearRequests[0].GetDeviceSetId())
	assert.Equal(t, "miner-a", deviceSets.clearRequests[0].GetDeviceIdentifier())
	assert.Empty(t, deviceSets.setRequests)
	assert.JSONEq(t, `{
		"cleared":true,
		"rack_id":21,
		"rack_label":"A1",
		"requested_count":1,
		"cleared_count":1,
		"device_identifiers":["miner-a"]
	}`, output.Content)
}

func TestWriteConfirmationRejectsArgumentsThatWouldNotBeExecuted(t *testing.T) {
	tools := NewFleetTools(nil, &recordingSitesHandler{}, nil, nil)

	confirmation, err := tools.Confirmation("create_site", json.RawMessage(`{"name":"North","unexpected":true}`))

	require.Error(t, err)
	assert.Nil(t, confirmation)
}
