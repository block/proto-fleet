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
}

func (*recordingDeviceSetsHandler) ListDeviceSets(context.Context, *connect.Request[devicesetv1.ListDeviceSetsRequest]) (*connect.Response[devicesetv1.ListDeviceSetsResponse], error) {
	return connect.NewResponse(&devicesetv1.ListDeviceSetsResponse{}), nil
}

func (h *recordingDeviceSetsHandler) CreateDeviceSet(_ context.Context, request *connect.Request[devicesetv1.CreateDeviceSetRequest]) (*connect.Response[devicesetv1.CreateDeviceSetResponse], error) {
	h.createRequest = request.Msg
	return connect.NewResponse(&devicesetv1.CreateDeviceSetResponse{DeviceSet: &devicesetv1.DeviceSet{Id: 21, Label: request.Msg.GetLabel()}}), nil
}

func (h *recordingDeviceSetsHandler) AssignDevicesToRack(_ context.Context, request *connect.Request[devicesetv1.AssignDevicesToRackRequest]) (*connect.Response[devicesetv1.AssignDevicesToRackResponse], error) {
	h.moveRequest = request.Msg
	return connect.NewResponse(&devicesetv1.AssignDevicesToRackResponse{AssignedCount: 2, RemovedCount: 1}), nil
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
	assert.False(t, requiresConfirmation["resolve_miners"])
	assert.False(t, requiresConfirmation["list_sites"])
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

func TestWriteConfirmationRejectsArgumentsThatWouldNotBeExecuted(t *testing.T) {
	tools := NewFleetTools(nil, &recordingSitesHandler{}, nil, nil)

	confirmation, err := tools.Confirmation("create_site", json.RawMessage(`{"name":"North","unexpected":true}`))

	require.Error(t, err)
	assert.Nil(t, confirmation)
}
