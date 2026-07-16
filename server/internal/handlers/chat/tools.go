package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"connectrpc.com/connect"

	fleetv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	poolsv1 "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	sitesv1 "github.com/block/proto-fleet/server/generated/grpc/sites/v1"
	chatdomain "github.com/block/proto-fleet/server/internal/domain/chat"
)

type fleetCountsHandler interface {
	GetMinerStateCounts(ctx context.Context, req *connect.Request[fleetv1.GetMinerStateCountsRequest]) (*connect.Response[fleetv1.GetMinerStateCountsResponse], error)
}

type sitesHandler interface {
	ListSites(ctx context.Context, req *connect.Request[sitesv1.ListSitesRequest]) (*connect.Response[sitesv1.ListSitesResponse], error)
}

type poolsHandler interface {
	ListPools(ctx context.Context, req *connect.Request[poolsv1.ListPoolsRequest]) (*connect.Response[poolsv1.ListPoolsResponse], error)
}

type FleetTools struct {
	fleet fleetCountsHandler
	sites sitesHandler
	pools poolsHandler
}

func NewFleetTools(fleet fleetCountsHandler, sites sitesHandler, pools poolsHandler) *FleetTools {
	return &FleetTools{fleet: fleet, sites: sites, pools: pools}
}

func (t *FleetTools) Definitions() []chatdomain.ToolDefinition {
	return []chatdomain.ToolDefinition{
		{
			Name:        "get_miner_state_counts",
			Description: "Get current counts of hashing, broken, offline, and sleeping miners, optionally filtered by site IDs.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"site_ids": map[string]any{
						"type": "array", "items": map[string]any{"type": "integer"},
					},
					"include_unassigned": map[string]any{"type": "boolean"},
				},
			},
		},
		{
			Name:        "list_sites",
			Description: "List sites in the organization with device, building, rack, and infrastructure counts.",
			InputSchema: emptyObjectSchema(),
		},
		{
			Name:        "list_pools",
			Description: "List saved mining pool names. Connection URLs, usernames, wallet identifiers, worker identifiers, and credentials are never returned.",
			InputSchema: emptyObjectSchema(),
		},
	}
}

func emptyObjectSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}}
}

func (t *FleetTools) Execute(ctx context.Context, name string, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	switch name {
	case "get_miner_state_counts":
		return t.getMinerStateCounts(ctx, arguments)
	case "list_sites":
		return t.listSites(ctx)
	case "list_pools":
		return t.listPools(ctx)
	default:
		return chatdomain.ToolOutput{}, fmt.Errorf("unknown tool %q", name)
	}
}

func (t *FleetTools) getMinerStateCounts(ctx context.Context, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	var input struct {
		SiteIDs           []int64 `json:"site_ids"`
		IncludeUnassigned bool    `json:"include_unassigned"`
	}
	if len(arguments) != 0 {
		decoder := json.NewDecoder(bytes.NewReader(arguments))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			return chatdomain.ToolOutput{}, fmt.Errorf("invalid get_miner_state_counts arguments: %w", err)
		}
	}
	response, err := t.fleet.GetMinerStateCounts(ctx, connect.NewRequest(&fleetv1.GetMinerStateCountsRequest{
		SiteIds:           input.SiteIDs,
		IncludeUnassigned: input.IncludeUnassigned,
	}))
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	counts := response.Msg.GetStateCounts()
	payload := map[string]any{
		"total_miners": response.Msg.GetTotalMiners(),
		"hashing":      counts.GetHashingCount(),
		"broken":       counts.GetBrokenCount(),
		"offline":      counts.GetOfflineCount(),
		"sleeping":     counts.GetSleepingCount(),
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal miner state counts: %w", err)
	}
	return chatdomain.ToolOutput{
		Content: string(content),
		Summary: fmt.Sprintf("Read state for %d miners", response.Msg.GetTotalMiners()),
	}, nil
}

func (t *FleetTools) listSites(ctx context.Context) (chatdomain.ToolOutput, error) {
	response, err := t.sites.ListSites(ctx, connect.NewRequest(&sitesv1.ListSitesRequest{}))
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	type siteView struct {
		ID             int64  `json:"id"`
		Name           string `json:"name"`
		DeviceCount    int64  `json:"device_count"`
		BuildingCount  int64  `json:"building_count"`
		RackCount      int64  `json:"rack_count"`
		Infrastructure int64  `json:"infrastructure_device_count"`
	}
	sites := make([]siteView, 0, len(response.Msg.GetSites()))
	for _, item := range response.Msg.GetSites() {
		sites = append(sites, siteView{
			ID:             item.GetSite().GetId(),
			Name:           item.GetSite().GetName(),
			DeviceCount:    item.GetDeviceCount(),
			BuildingCount:  item.GetBuildingCount(),
			RackCount:      item.GetRackCount(),
			Infrastructure: item.GetInfrastructureDeviceCount(),
		})
	}
	content, err := json.Marshal(map[string]any{"sites": sites})
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal sites: %w", err)
	}
	return chatdomain.ToolOutput{Content: string(content), Summary: fmt.Sprintf("Read %d sites", len(sites))}, nil
}

func (t *FleetTools) listPools(ctx context.Context) (chatdomain.ToolOutput, error) {
	response, err := t.pools.ListPools(ctx, connect.NewRequest(&poolsv1.ListPoolsRequest{}))
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	type poolView struct {
		Name string `json:"name"`
	}
	pools := make([]poolView, 0, len(response.Msg.GetPools()))
	for _, pool := range response.Msg.GetPools() {
		pools = append(pools, poolView{Name: pool.GetPoolName()})
	}
	content, err := json.Marshal(map[string]any{"pools": pools})
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal pools: %w", err)
	}
	return chatdomain.ToolOutput{Content: string(content), Summary: fmt.Sprintf("Read %d pools", len(pools))}, nil
}
