package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"

	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	devicesetv1 "github.com/block/proto-fleet/server/generated/grpc/device_set/v1"
	fleetv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	minercommandv1 "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	poolsv1 "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	schedulev1 "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
	sitesv1 "github.com/block/proto-fleet/server/generated/grpc/sites/v1"
	chatdomain "github.com/block/proto-fleet/server/internal/domain/chat"
)

const (
	defaultResolveMinersLimit = 100
	maxResolveMinersLimit     = 1000
	maxResolveMinersScan      = 5000
	maxRackSlotAssignments    = 1000
	maxMinerActionDevices     = 1000
	maxDowntimeTargets        = 1000
)

const (
	rackOrderIndexBottomLeft  = "bottom_left"
	rackOrderIndexTopLeft     = "top_left"
	rackOrderIndexBottomRight = "bottom_right"
	rackOrderIndexTopRight    = "top_right"
	labelUnspecified          = "unspecified"
)

const (
	minerActionSelectorExplicit   = "explicit"
	minerActionSelectorAllDevices = "all_devices"
	minerActionSelectorFilter     = "filter"
)

type fleetHandler interface {
	GetMinerStateCounts(ctx context.Context, req *connect.Request[fleetv1.GetMinerStateCountsRequest]) (*connect.Response[fleetv1.GetMinerStateCountsResponse], error)
	ListMinerStateSnapshots(ctx context.Context, req *connect.Request[fleetv1.ListMinerStateSnapshotsRequest]) (*connect.Response[fleetv1.ListMinerStateSnapshotsResponse], error)
}

type sitesHandler interface {
	ListSites(ctx context.Context, req *connect.Request[sitesv1.ListSitesRequest]) (*connect.Response[sitesv1.ListSitesResponse], error)
	CreateSite(ctx context.Context, req *connect.Request[sitesv1.CreateSiteRequest]) (*connect.Response[sitesv1.CreateSiteResponse], error)
}

type poolsHandler interface {
	ListPools(ctx context.Context, req *connect.Request[poolsv1.ListPoolsRequest]) (*connect.Response[poolsv1.ListPoolsResponse], error)
}

type deviceSetsHandler interface {
	GetDeviceSet(ctx context.Context, req *connect.Request[devicesetv1.GetDeviceSetRequest]) (*connect.Response[devicesetv1.GetDeviceSetResponse], error)
	ListDeviceSets(ctx context.Context, req *connect.Request[devicesetv1.ListDeviceSetsRequest]) (*connect.Response[devicesetv1.ListDeviceSetsResponse], error)
	ListDeviceSetMembers(ctx context.Context, req *connect.Request[devicesetv1.ListDeviceSetMembersRequest]) (*connect.Response[devicesetv1.ListDeviceSetMembersResponse], error)
	CreateDeviceSet(ctx context.Context, req *connect.Request[devicesetv1.CreateDeviceSetRequest]) (*connect.Response[devicesetv1.CreateDeviceSetResponse], error)
	AssignDevicesToRack(ctx context.Context, req *connect.Request[devicesetv1.AssignDevicesToRackRequest]) (*connect.Response[devicesetv1.AssignDevicesToRackResponse], error)
	SetRackSlotPosition(ctx context.Context, req *connect.Request[devicesetv1.SetRackSlotPositionRequest]) (*connect.Response[devicesetv1.SetRackSlotPositionResponse], error)
	ClearRackSlotPosition(ctx context.Context, req *connect.Request[devicesetv1.ClearRackSlotPositionRequest]) (*connect.Response[devicesetv1.ClearRackSlotPositionResponse], error)
	GetRackSlots(ctx context.Context, req *connect.Request[devicesetv1.GetRackSlotsRequest]) (*connect.Response[devicesetv1.GetRackSlotsResponse], error)
}

type commandHandler interface {
	CheckCommandCapabilities(ctx context.Context, req *connect.Request[minercommandv1.CheckCommandCapabilitiesRequest]) (*connect.Response[minercommandv1.CheckCommandCapabilitiesResponse], error)
	Reboot(ctx context.Context, req *connect.Request[minercommandv1.RebootRequest]) (*connect.Response[minercommandv1.RebootResponse], error)
	StopMining(ctx context.Context, req *connect.Request[minercommandv1.StopMiningRequest]) (*connect.Response[minercommandv1.StopMiningResponse], error)
	StartMining(ctx context.Context, req *connect.Request[minercommandv1.StartMiningRequest]) (*connect.Response[minercommandv1.StartMiningResponse], error)
	BlinkLED(ctx context.Context, req *connect.Request[minercommandv1.BlinkLEDRequest]) (*connect.Response[minercommandv1.BlinkLEDResponse], error)
}

type scheduleHandler interface {
	CreateSchedule(ctx context.Context, req *connect.Request[schedulev1.CreateScheduleRequest]) (*connect.Response[schedulev1.CreateScheduleResponse], error)
}

type FleetTools struct {
	fleet      fleetHandler
	sites      sitesHandler
	pools      poolsHandler
	deviceSets deviceSetsHandler
	commands   commandHandler
	schedules  scheduleHandler
}

func NewFleetTools(
	fleet fleetHandler,
	sites sitesHandler,
	pools poolsHandler,
	deviceSets deviceSetsHandler,
	commands commandHandler,
	schedules scheduleHandler,
) *FleetTools {
	return &FleetTools{
		fleet:      fleet,
		sites:      sites,
		pools:      pools,
		deviceSets: deviceSets,
		commands:   commands,
		schedules:  schedules,
	}
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
			Name:        "get_site_health_summary",
			Description: "Get a structured health summary for the fleet or one site, including current miner state counts and site inventory rows.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"site_id":            map[string]any{"type": "integer", "minimum": 1},
					"include_unassigned": map[string]any{"type": "boolean"},
				},
			},
		},
		{
			Name:        "resolve_miners",
			Description: "Resolve miner descriptions into explicit device identifiers for follow-up write tools. Use this before move_miners_to_rack unless the operator supplied exact device identifiers.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"maxLength":   255,
						"description": "Case-insensitive text matched against returned miner identifiers, names, MACs, serials, IPs, and models after server filters.",
					},
					"device_statuses": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
							"enum": []string{
								"online", "offline", "maintenance", "error", "inactive",
								"needs_mining_pool", "updating", "reboot_required",
								"hashing", "broken", "sleeping",
							},
						},
						"uniqueItems": true,
					},
					"site_ids":            map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}},
					"include_unassigned":  map[string]any{"type": "boolean"},
					"building_ids":        map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}},
					"include_no_building": map[string]any{"type": "boolean"},
					"rack_ids":            map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}},
					"include_no_rack":     map[string]any{"type": "boolean"},
					"models":              map[string]any{"type": "array", "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 255}},
					"ip_cidrs":            map[string]any{"type": "array", "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 64}},
					"limit":               map[string]any{"type": "integer", "minimum": 1, "maximum": maxResolveMinersLimit, "description": "Maximum miners to return. Use 1000 when resolving all matching miners for a write."},
				},
			},
		},
		{
			Name:        "list_actionable_miner_issues",
			Description: "List miners currently in actionable states with concise suggested next actions. Use this to triage offline, broken, pool-missing, reboot-required, updating, inactive, or maintenance miners.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"maxLength":   255,
						"description": "Optional case-insensitive text matched against returned miner identifiers, names, MACs, serials, IPs, and models after server filters.",
					},
					"device_statuses": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
							"enum": []string{
								"offline", "maintenance", "error", "inactive",
								"needs_mining_pool", "updating", "reboot_required",
								"broken", "sleeping",
							},
						},
						"uniqueItems": true,
						"description": "Defaults to offline, broken, needs_mining_pool, and reboot_required.",
					},
					"site_ids":            map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}},
					"include_unassigned":  map[string]any{"type": "boolean"},
					"building_ids":        map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}},
					"include_no_building": map[string]any{"type": "boolean"},
					"rack_ids":            map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}},
					"include_no_rack":     map[string]any{"type": "boolean"},
					"models":              map[string]any{"type": "array", "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 255}},
					"ip_cidrs":            map[string]any{"type": "array", "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 64}},
					"limit":               map[string]any{"type": "integer", "minimum": 1, "maximum": maxResolveMinersLimit},
				},
			},
		},
		{
			Name:        "list_pools",
			Description: "List saved mining pool names. Connection URLs, usernames, wallet identifiers, worker identifiers, and credentials are never returned.",
			InputSchema: emptyObjectSchema(),
		},
		{
			Name:        "list_racks",
			Description: "List racks with IDs, labels, layouts, numbering origin, placement labels, and miner counts so an operator request can be resolved to an exact rack.",
			InputSchema: emptyObjectSchema(),
		},
		{
			Name:        "get_rack_slots",
			Description: "List occupied slot positions for one rack. Slot row and column coordinates are 0-indexed.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"rack_id"},
				"properties": map[string]any{
					"rack_id": map[string]any{"type": "integer", "minimum": 1},
				},
			},
		},
		{
			Name:        "get_rack_health",
			Description: "Get rack layout, slot occupancy, member list, and current miner status counts for one rack.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"rack_id"},
				"properties": map[string]any{
					"rack_id": map[string]any{"type": "integer", "minimum": 1},
					"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": maxResolveMinersLimit, "description": "Maximum rack member rows to return."},
				},
			},
		},
		{
			Name:        "preview_miner_action",
			Description: "Check whether selected miners support a command before requesting execution. Use selector.type all_devices for whole-fleet actions, filter for backend-resolvable subsets, or explicit for exact miner IDs.",
			InputSchema: minerActionSchema(),
		},
		{
			Name:                 "execute_miner_action",
			Description:          "Execute a miner command on selected miners. Use selector.type all_devices for whole-fleet actions, filter for backend-resolvable subsets, or explicit for exact miner IDs. Use preview_miner_action first when possible. This write always pauses for explicit operator confirmation before execution.",
			RequiresConfirmation: true,
			InputSchema:          minerActionSchema(),
		},
		{
			Name:        "preview_downtime_window",
			Description: "Preview a one-time maintenance schedule using the existing schedule service. The sleep action stops mining at the start time and does not automatically resume at end_time.",
			InputSchema: downtimeWindowSchema(),
		},
		{
			Name:                 "create_downtime_window",
			Description:          "Create a one-time maintenance schedule using the existing schedule service. The sleep action stops mining at the start time and does not automatically resume at end_time. This write always pauses for explicit operator confirmation before execution.",
			RequiresConfirmation: true,
			InputSchema:          downtimeWindowSchema(),
		},
		{
			Name:                 "create_site",
			Description:          "Create a site. This write always pauses for explicit operator confirmation before execution.",
			RequiresConfirmation: true,
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"name"},
				"properties": map[string]any{
					"name":              map[string]any{"type": "string", "minLength": 1, "maxLength": 255},
					"location_city":     map[string]any{"type": "string", "maxLength": 255},
					"location_state":    map[string]any{"type": "string", "maxLength": 255},
					"country":           map[string]any{"type": "string", "maxLength": 2},
					"address":           map[string]any{"type": "string", "maxLength": 255},
					"postal_code":       map[string]any{"type": "string", "maxLength": 32},
					"timezone":          map[string]any{"type": "string", "maxLength": 64},
					"power_capacity_mw": map[string]any{"type": "number", "minimum": 0},
					"notes":             map[string]any{"type": "string", "maxLength": 4096},
				},
			},
		},
		{
			Name:                 "create_rack",
			Description:          "Create an empty rack with a grid layout and optional site or building placement. This write always pauses for explicit operator confirmation before execution.",
			RequiresConfirmation: true,
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"label", "rows", "columns"},
				"properties": map[string]any{
					"label":        map[string]any{"type": "string", "minLength": 1, "maxLength": 100},
					"rows":         map[string]any{"type": "integer", "minimum": 1},
					"columns":      map[string]any{"type": "integer", "minimum": 1},
					"zone":         map[string]any{"type": "string", "maxLength": 100},
					"site_id":      map[string]any{"type": "integer", "minimum": 1},
					"building_id":  map[string]any{"type": "integer", "minimum": 1},
					"order_index":  map[string]any{"type": "string", "enum": []string{rackOrderIndexTopLeft, rackOrderIndexTopRight, rackOrderIndexBottomLeft, rackOrderIndexBottomRight}},
					"cooling_type": map[string]any{"type": "string", "enum": []string{"air", "immersion"}},
				},
			},
		},
		{
			Name:                 "move_miners_to_rack",
			Description:          "Atomically move explicitly identified miners to an existing rack. This write always pauses for explicit operator confirmation before execution.",
			RequiresConfirmation: true,
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"target_rack_id", "device_identifiers"},
				"properties": map[string]any{
					"target_rack_id": map[string]any{"type": "integer", "minimum": 1},
					"device_identifiers": map[string]any{
						"type": "array", "minItems": 1, "maxItems": 1000, "uniqueItems": true,
						"items": map[string]any{"type": "string", "minLength": 1, "maxLength": 255},
					},
					"force_clear_conflicting_site": map[string]any{"type": "boolean"},
				},
			},
		},
		{
			Name:                 "set_rack_slots",
			Description:          "Assign explicitly identified miners that already belong to a rack to specific 0-indexed row/column slot positions. Use resolve_miners with rack_ids first unless exact device identifiers are already supplied; use list_racks to convert human-facing slot numbers according to the rack layout and numbering origin. This write always pauses for explicit operator confirmation before execution.",
			RequiresConfirmation: true,
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"rack_id", "slot_assignments"},
				"properties": map[string]any{
					"rack_id": map[string]any{"type": "integer", "minimum": 1},
					"slot_assignments": map[string]any{
						"type": "array", "minItems": 1, "maxItems": maxRackSlotAssignments,
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []string{"device_identifier", "row", "column"},
							"properties": map[string]any{
								"device_identifier": map[string]any{"type": "string", "minLength": 1, "maxLength": 255},
								"row":               map[string]any{"type": "integer", "minimum": 0, "description": "0-indexed row coordinate."},
								"column":            map[string]any{"type": "integer", "minimum": 0, "description": "0-indexed column coordinate."},
							},
						},
					},
				},
			},
		},
		{
			Name:                 "clear_rack_slots",
			Description:          "Clear slot positions for explicitly identified miners in a rack while preserving rack membership. This write always pauses for explicit operator confirmation before execution.",
			RequiresConfirmation: true,
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"rack_id", "device_identifiers"},
				"properties": map[string]any{
					"rack_id": map[string]any{"type": "integer", "minimum": 1},
					"device_identifiers": map[string]any{
						"type": "array", "minItems": 1, "maxItems": maxRackSlotAssignments, "uniqueItems": true,
						"items": map[string]any{"type": "string", "minLength": 1, "maxLength": 255},
					},
				},
			},
		},
	}
}

func emptyObjectSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}}
}

func minerActionSchema() map[string]any {
	deviceIdentifiersSchema := map[string]any{
		"type": "array", "minItems": 1, "maxItems": maxMinerActionDevices, "uniqueItems": true,
		"items": map[string]any{"type": "string", "minLength": 1, "maxLength": 255},
	}
	filterSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"device_statuses": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
					"enum": []string{
						"online", "offline", "maintenance", "error", "inactive",
						"needs_mining_pool", "updating", "reboot_required",
						"hashing", "broken", "sleeping",
					},
				},
				"uniqueItems": true,
			},
			"site_ids":            map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}},
			"include_unassigned":  map[string]any{"type": "boolean"},
			"building_ids":        map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}},
			"include_no_building": map[string]any{"type": "boolean"},
			"rack_ids":            map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}},
			"include_no_rack":     map[string]any{"type": "boolean"},
			"models":              map[string]any{"type": "array", "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 255}},
			"ip_cidrs":            map[string]any{"type": "array", "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 64}},
		},
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action"},
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{"reboot", "start_mining", "stop_mining", "blink_led"},
			},
			"device_identifiers": deviceIdentifiersSchema,
			"selector": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"type"},
				"properties": map[string]any{
					"type":               map[string]any{"type": "string", "enum": []string{minerActionSelectorExplicit, minerActionSelectorAllDevices, minerActionSelectorFilter}},
					"device_identifiers": deviceIdentifiersSchema,
					"filter":             filterSchema,
				},
			},
		},
	}
}

func downtimeWindowSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"name", "action", "start_date", "start_time", "timezone", "targets"},
		"properties": map[string]any{
			"name":       map[string]any{"type": "string", "minLength": 1, "maxLength": 100},
			"action":     map[string]any{"type": "string", "enum": []string{"sleep", "reboot", "set_power_target"}},
			"start_date": map[string]any{"type": "string", "description": "YYYY-MM-DD"},
			"start_time": map[string]any{"type": "string", "description": "HH:MM, 24-hour local time in timezone"},
			"end_date":   map[string]any{"type": "string", "description": "Optional YYYY-MM-DD"},
			"end_time":   map[string]any{"type": "string", "description": "Optional HH:MM. Only bounded set_power_target windows auto-revert at end_time."},
			"timezone":   map[string]any{"type": "string", "description": "IANA timezone, for example America/Chicago"},
			"power_target_mode": map[string]any{
				"type":        "string",
				"enum":        []string{"default", "max"},
				"description": "Required only when action is set_power_target.",
			},
			"targets": map[string]any{
				"type": "array", "minItems": 1, "maxItems": maxDowntimeTargets,
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"type", "target_id"},
					"properties": map[string]any{
						"type":      map[string]any{"type": "string", "enum": []string{"rack", "miner", "group", "site", "building"}},
						"target_id": map[string]any{"type": "string", "minLength": 1},
					},
				},
			},
		},
	}
}

type createSiteInput struct {
	Name            string  `json:"name"`
	LocationCity    string  `json:"location_city"`
	LocationState   string  `json:"location_state"`
	Country         string  `json:"country"`
	Address         string  `json:"address"`
	PostalCode      string  `json:"postal_code"`
	Timezone        string  `json:"timezone"`
	PowerCapacityMW float64 `json:"power_capacity_mw"`
	Notes           string  `json:"notes"`
}

type resolveMinersInput struct {
	Query             string   `json:"query"`
	DeviceStatuses    []string `json:"device_statuses"`
	SiteIDs           []int64  `json:"site_ids"`
	IncludeUnassigned bool     `json:"include_unassigned"`
	BuildingIDs       []int64  `json:"building_ids"`
	IncludeNoBuilding bool     `json:"include_no_building"`
	RackIDs           []int64  `json:"rack_ids"`
	IncludeNoRack     bool     `json:"include_no_rack"`
	Models            []string `json:"models"`
	IPCIDRs           []string `json:"ip_cidrs"`
	Limit             int32    `json:"limit"`
}

type rackHealthInput struct {
	RackID int64 `json:"rack_id"`
	Limit  int32 `json:"limit"`
}

type minerActionInput struct {
	Action            string                    `json:"action"`
	DeviceIdentifiers []string                  `json:"device_identifiers"`
	Selector          *minerActionSelectorInput `json:"selector"`
}

type minerActionSelectorInput struct {
	Type              string                  `json:"type"`
	DeviceIdentifiers []string                `json:"device_identifiers"`
	Filter            *minerActionFilterInput `json:"filter"`
}

type minerActionFilterInput struct {
	DeviceStatuses    []string `json:"device_statuses"`
	SiteIDs           []int64  `json:"site_ids"`
	IncludeUnassigned bool     `json:"include_unassigned"`
	BuildingIDs       []int64  `json:"building_ids"`
	IncludeNoBuilding bool     `json:"include_no_building"`
	RackIDs           []int64  `json:"rack_ids"`
	IncludeNoRack     bool     `json:"include_no_rack"`
	Models            []string `json:"models"`
	IPCIDRs           []string `json:"ip_cidrs"`
}

type minerActionSelectionView struct {
	Type              string                  `json:"type"`
	Description       string                  `json:"description"`
	DeviceIdentifiers []string                `json:"device_identifiers,omitempty"`
	Filter            *minerActionFilterInput `json:"filter,omitempty"`
}

type downtimeTargetInput struct {
	Type     string `json:"type"`
	TargetID string `json:"target_id"`
}

type downtimeWindowInput struct {
	Name            string                `json:"name"`
	Action          string                `json:"action"`
	StartDate       string                `json:"start_date"`
	StartTime       string                `json:"start_time"`
	EndDate         string                `json:"end_date"`
	EndTime         string                `json:"end_time"`
	Timezone        string                `json:"timezone"`
	PowerTargetMode string                `json:"power_target_mode"`
	Targets         []downtimeTargetInput `json:"targets"`
}

type createRackInput struct {
	Label       string `json:"label"`
	Rows        int32  `json:"rows"`
	Columns     int32  `json:"columns"`
	Zone        string `json:"zone"`
	SiteID      *int64 `json:"site_id"`
	BuildingID  *int64 `json:"building_id"`
	OrderIndex  string `json:"order_index"`
	CoolingType string `json:"cooling_type"`
}

type moveMinersToRackInput struct {
	TargetRackID              int64    `json:"target_rack_id"`
	DeviceIdentifiers         []string `json:"device_identifiers"`
	ForceClearConflictingSite bool     `json:"force_clear_conflicting_site"`
}

type rackSlotAssignmentInput struct {
	DeviceIdentifier string `json:"device_identifier"`
	Row              int32  `json:"row"`
	Column           int32  `json:"column"`
}

type setRackSlotsInput struct {
	RackID          int64                     `json:"rack_id"`
	SlotAssignments []rackSlotAssignmentInput `json:"slot_assignments"`
}

type clearRackSlotsInput struct {
	RackID            int64    `json:"rack_id"`
	DeviceIdentifiers []string `json:"device_identifiers"`
}

func decodeToolArguments(arguments json.RawMessage, destination any) error {
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode tool arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return fmt.Errorf("check for trailing JSON values: %w", err)
	}
	return nil
}

func buildCreateSiteRequest(arguments json.RawMessage) (createSiteInput, *sitesv1.CreateSiteRequest, error) {
	var input createSiteInput
	if err := decodeToolArguments(arguments, &input); err != nil {
		return input, nil, fmt.Errorf("invalid create_site arguments: %w", err)
	}
	request := &sitesv1.CreateSiteRequest{
		Name:            input.Name,
		LocationCity:    input.LocationCity,
		LocationState:   input.LocationState,
		Country:         input.Country,
		Address:         input.Address,
		PostalCode:      input.PostalCode,
		Timezone:        input.Timezone,
		PowerCapacityMw: input.PowerCapacityMW,
		Notes:           input.Notes,
	}
	if err := protovalidate.Validate(request); err != nil {
		return input, nil, fmt.Errorf("invalid create_site arguments: %w", err)
	}
	return input, request, nil
}

func buildResolveMinersFilter(arguments json.RawMessage) (resolveMinersInput, *fleetv1.MinerListFilter, error) {
	var input resolveMinersInput
	if len(arguments) != 0 {
		if err := decodeToolArguments(arguments, &input); err != nil {
			return input, nil, fmt.Errorf("invalid resolve_miners arguments: %w", err)
		}
	}
	input.Query = strings.TrimSpace(input.Query)
	if len(input.Query) > 255 {
		return input, nil, fmt.Errorf("invalid resolve_miners arguments: query must be 255 characters or fewer")
	}
	if input.Limit == 0 {
		input.Limit = defaultResolveMinersLimit
	}
	if input.Limit < 1 || input.Limit > maxResolveMinersLimit {
		return input, nil, fmt.Errorf("invalid resolve_miners arguments: limit must be between 1 and %d", maxResolveMinersLimit)
	}
	if err := validatePositiveIDs("site_ids", input.SiteIDs); err != nil {
		return input, nil, err
	}
	if err := validatePositiveIDs("building_ids", input.BuildingIDs); err != nil {
		return input, nil, err
	}
	if err := validatePositiveIDs("rack_ids", input.RackIDs); err != nil {
		return input, nil, err
	}
	statuses := make([]fleetv1.DeviceStatus, 0, len(input.DeviceStatuses))
	for _, status := range input.DeviceStatuses {
		parsed, err := parseResolveMinerStatus(status)
		if err != nil {
			return input, nil, err
		}
		statuses = append(statuses, parsed)
	}
	filter := &fleetv1.MinerListFilter{
		DeviceStatus:      statuses,
		SiteIds:           input.SiteIDs,
		IncludeUnassigned: input.IncludeUnassigned,
		BuildingIds:       input.BuildingIDs,
		IncludeNoBuilding: input.IncludeNoBuilding,
		RackIds:           input.RackIDs,
		IncludeNoRack:     input.IncludeNoRack,
		Models:            input.Models,
		IpCidrs:           input.IPCIDRs,
	}
	return input, filter, nil
}

func validatePositiveIDs(field string, ids []int64) error {
	return validatePositiveIDsForTool("resolve_miners", field, ids)
}

func validatePositiveIDsForTool(toolName, field string, ids []int64) error {
	for _, id := range ids {
		if id <= 0 {
			return fmt.Errorf("invalid %s arguments: %s must contain only positive IDs", toolName, field)
		}
	}
	return nil
}

func parseResolveMinerStatus(status string) (fleetv1.DeviceStatus, error) {
	return parseDeviceStatusArgument("resolve_miners", status)
}

func parseDeviceStatusArgument(toolName, status string) (fleetv1.DeviceStatus, error) {
	normalized := strings.ToLower(strings.TrimSpace(status))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	switch normalized {
	case "online", "hashing":
		return fleetv1.DeviceStatus_DEVICE_STATUS_ONLINE, nil
	case "offline":
		return fleetv1.DeviceStatus_DEVICE_STATUS_OFFLINE, nil
	case "maintenance":
		return fleetv1.DeviceStatus_DEVICE_STATUS_MAINTENANCE, nil
	case "error", "broken":
		return fleetv1.DeviceStatus_DEVICE_STATUS_ERROR, nil
	case "inactive", "sleeping":
		return fleetv1.DeviceStatus_DEVICE_STATUS_INACTIVE, nil
	case "needs_mining_pool", "needs_pool", "pool_needed":
		return fleetv1.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL, nil
	case "updating":
		return fleetv1.DeviceStatus_DEVICE_STATUS_UPDATING, nil
	case "reboot_required":
		return fleetv1.DeviceStatus_DEVICE_STATUS_REBOOT_REQUIRED, nil
	default:
		return fleetv1.DeviceStatus_DEVICE_STATUS_UNSPECIFIED, fmt.Errorf("invalid %s arguments: unsupported device_status %q", toolName, status)
	}
}

func buildCreateRackRequest(arguments json.RawMessage) (createRackInput, *devicesetv1.CreateDeviceSetRequest, error) {
	var input createRackInput
	if err := decodeToolArguments(arguments, &input); err != nil {
		return input, nil, fmt.Errorf("invalid create_rack arguments: %w", err)
	}
	if input.SiteID != nil && input.BuildingID != nil {
		return input, nil, fmt.Errorf("invalid create_rack arguments: specify site_id or building_id, not both")
	}
	if input.SiteID != nil && *input.SiteID <= 0 {
		return input, nil, fmt.Errorf("invalid create_rack arguments: site_id must be positive")
	}
	if input.BuildingID != nil && *input.BuildingID <= 0 {
		return input, nil, fmt.Errorf("invalid create_rack arguments: building_id must be positive")
	}
	orderIndex := devicesetv1.RackOrderIndex_RACK_ORDER_INDEX_TOP_LEFT
	switch input.OrderIndex {
	case "", rackOrderIndexTopLeft:
	case rackOrderIndexTopRight:
		orderIndex = devicesetv1.RackOrderIndex_RACK_ORDER_INDEX_TOP_RIGHT
	case rackOrderIndexBottomLeft:
		orderIndex = devicesetv1.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT
	case rackOrderIndexBottomRight:
		orderIndex = devicesetv1.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_RIGHT
	default:
		return input, nil, fmt.Errorf("invalid create_rack order_index %q", input.OrderIndex)
	}
	coolingType := devicesetv1.RackCoolingType_RACK_COOLING_TYPE_AIR
	switch input.CoolingType {
	case "", "air":
	case "immersion":
		coolingType = devicesetv1.RackCoolingType_RACK_COOLING_TYPE_IMMERSION
	default:
		return input, nil, fmt.Errorf("invalid create_rack cooling_type %q", input.CoolingType)
	}
	request := &devicesetv1.CreateDeviceSetRequest{
		Type:  devicesetv1.DeviceSetType_DEVICE_SET_TYPE_RACK,
		Label: input.Label,
		TypeDetails: &devicesetv1.CreateDeviceSetRequest_RackInfo{RackInfo: &devicesetv1.RackInfo{
			Rows:        input.Rows,
			Columns:     input.Columns,
			Zone:        input.Zone,
			OrderIndex:  orderIndex,
			CoolingType: coolingType,
			SiteId:      input.SiteID,
			BuildingId:  input.BuildingID,
		}},
	}
	if err := protovalidate.Validate(request); err != nil {
		return input, nil, fmt.Errorf("invalid create_rack arguments: %w", err)
	}
	return input, request, nil
}

func buildMoveMinersRequest(arguments json.RawMessage) (moveMinersToRackInput, *devicesetv1.AssignDevicesToRackRequest, error) {
	var input moveMinersToRackInput
	if err := decodeToolArguments(arguments, &input); err != nil {
		return input, nil, fmt.Errorf("invalid move_miners_to_rack arguments: %w", err)
	}
	if len(input.DeviceIdentifiers) == 0 || len(input.DeviceIdentifiers) > 1000 {
		return input, nil, fmt.Errorf("invalid move_miners_to_rack arguments: device_identifiers must contain between 1 and 1000 miners")
	}
	seen := make(map[string]struct{}, len(input.DeviceIdentifiers))
	for _, identifier := range input.DeviceIdentifiers {
		if strings.TrimSpace(identifier) == "" || len(identifier) > 255 {
			return input, nil, fmt.Errorf("invalid move_miners_to_rack arguments: device identifiers must contain 1 to 255 characters")
		}
		if _, exists := seen[identifier]; exists {
			return input, nil, fmt.Errorf("invalid move_miners_to_rack arguments: duplicate device identifier %q", identifier)
		}
		seen[identifier] = struct{}{}
	}
	force := input.ForceClearConflictingSite
	request := &devicesetv1.AssignDevicesToRackRequest{
		TargetRackId: &input.TargetRackID,
		DeviceSelector: &commonv1.DeviceSelector{
			SelectionType: &commonv1.DeviceSelector_DeviceList{DeviceList: &commonv1.DeviceIdentifierList{
				DeviceIdentifiers: input.DeviceIdentifiers,
			}},
		},
		ForceClearConflictingSite: &force,
	}
	if err := protovalidate.Validate(request); err != nil {
		return input, nil, fmt.Errorf("invalid move_miners_to_rack arguments: %w", err)
	}
	return input, request, nil
}

func buildGetRackSlotsRequest(arguments json.RawMessage) (int64, *devicesetv1.GetRackSlotsRequest, error) {
	var input struct {
		RackID int64 `json:"rack_id"`
	}
	if err := decodeToolArguments(arguments, &input); err != nil {
		return 0, nil, fmt.Errorf("invalid get_rack_slots arguments: %w", err)
	}
	if input.RackID <= 0 {
		return 0, nil, fmt.Errorf("invalid get_rack_slots arguments: rack_id must be positive")
	}
	request := &devicesetv1.GetRackSlotsRequest{DeviceSetId: input.RackID}
	if err := protovalidate.Validate(request); err != nil {
		return 0, nil, fmt.Errorf("invalid get_rack_slots arguments: %w", err)
	}
	return input.RackID, request, nil
}

func buildRackHealthInput(arguments json.RawMessage) (rackHealthInput, error) {
	var input rackHealthInput
	if err := decodeToolArguments(arguments, &input); err != nil {
		return input, fmt.Errorf("invalid get_rack_health arguments: %w", err)
	}
	if input.RackID <= 0 {
		return input, fmt.Errorf("invalid get_rack_health arguments: rack_id must be positive")
	}
	if input.Limit == 0 {
		input.Limit = maxResolveMinersLimit
	}
	if input.Limit < 1 || input.Limit > maxResolveMinersLimit {
		return input, fmt.Errorf("invalid get_rack_health arguments: limit must be between 1 and %d", maxResolveMinersLimit)
	}
	return input, nil
}

func buildMinerActionInput(arguments json.RawMessage, toolName string) (minerActionInput, minercommandv1.CommandType, *minercommandv1.DeviceSelector, minerActionSelectionView, error) {
	var input minerActionInput
	if err := decodeToolArguments(arguments, &input); err != nil {
		return input, minercommandv1.CommandType_COMMAND_TYPE_UNSPECIFIED, nil, minerActionSelectionView{}, fmt.Errorf("invalid %s arguments: %w", toolName, err)
	}
	commandType, err := parseMinerActionCommandType(input.Action)
	if err != nil {
		return input, minercommandv1.CommandType_COMMAND_TYPE_UNSPECIFIED, nil, minerActionSelectionView{}, fmt.Errorf("invalid %s arguments: %w", toolName, err)
	}
	selector, selection, err := buildMinerActionSelector(input, toolName)
	if err != nil {
		return input, minercommandv1.CommandType_COMMAND_TYPE_UNSPECIFIED, nil, minerActionSelectionView{}, err
	}
	return input, commandType, selector, selection, nil
}

func parseMinerActionCommandType(action string) (minercommandv1.CommandType, error) {
	switch normalizeToolEnum(action) {
	case "reboot":
		return minercommandv1.CommandType_COMMAND_TYPE_REBOOT, nil
	case "start_mining":
		return minercommandv1.CommandType_COMMAND_TYPE_START_MINING, nil
	case "stop_mining":
		return minercommandv1.CommandType_COMMAND_TYPE_STOP_MINING, nil
	case "blink_led":
		return minercommandv1.CommandType_COMMAND_TYPE_BLINK_LED, nil
	default:
		return minercommandv1.CommandType_COMMAND_TYPE_UNSPECIFIED, fmt.Errorf("unsupported action %q", action)
	}
}

func commandSelectorForDevices(deviceIdentifiers []string) *minercommandv1.DeviceSelector {
	return &minercommandv1.DeviceSelector{
		SelectionType: &minercommandv1.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonv1.DeviceIdentifierList{
				DeviceIdentifiers: deviceIdentifiers,
			},
		},
	}
}

func buildMinerActionSelector(input minerActionInput, toolName string) (*minercommandv1.DeviceSelector, minerActionSelectionView, error) {
	if input.Selector == nil {
		if err := validateExplicitDeviceIdentifiers(toolName, input.DeviceIdentifiers, maxMinerActionDevices); err != nil {
			return nil, minerActionSelectionView{}, err
		}
		return commandSelectorForDevices(input.DeviceIdentifiers), minerActionSelectionView{
			Type:              minerActionSelectorExplicit,
			Description:       fmt.Sprintf("%d explicit miner(s)", len(input.DeviceIdentifiers)),
			DeviceIdentifiers: input.DeviceIdentifiers,
		}, nil
	}
	if len(input.DeviceIdentifiers) > 0 {
		return nil, minerActionSelectionView{}, fmt.Errorf("invalid %s arguments: specify either top-level device_identifiers or selector, not both", toolName)
	}
	switch normalizeToolEnum(input.Selector.Type) {
	case minerActionSelectorExplicit:
		if err := validateExplicitDeviceIdentifiers(toolName, input.Selector.DeviceIdentifiers, maxMinerActionDevices); err != nil {
			return nil, minerActionSelectionView{}, err
		}
		if input.Selector.Filter != nil {
			return nil, minerActionSelectionView{}, fmt.Errorf("invalid %s arguments: selector.filter is only valid when selector.type is filter", toolName)
		}
		return commandSelectorForDevices(input.Selector.DeviceIdentifiers), minerActionSelectionView{
			Type:              minerActionSelectorExplicit,
			Description:       fmt.Sprintf("%d explicit miner(s)", len(input.Selector.DeviceIdentifiers)),
			DeviceIdentifiers: input.Selector.DeviceIdentifiers,
		}, nil
	case minerActionSelectorAllDevices:
		if len(input.Selector.DeviceIdentifiers) > 0 || input.Selector.Filter != nil {
			return nil, minerActionSelectionView{}, fmt.Errorf("invalid %s arguments: all_devices selector does not accept device_identifiers or filter", toolName)
		}
		return &minercommandv1.DeviceSelector{
				SelectionType: &minercommandv1.DeviceSelector_AllDevices{
					AllDevices: &minercommandv1.DeviceFilter{},
				},
			}, minerActionSelectionView{
				Type:        minerActionSelectorAllDevices,
				Description: "whole fleet command-eligible miners",
			}, nil
	case minerActionSelectorFilter:
		if len(input.Selector.DeviceIdentifiers) > 0 {
			return nil, minerActionSelectionView{}, fmt.Errorf("invalid %s arguments: selector.device_identifiers is only valid when selector.type is explicit", toolName)
		}
		filter, err := buildMinerActionFilter(input.Selector.Filter, toolName)
		if err != nil {
			return nil, minerActionSelectionView{}, err
		}
		filterInput := input.Selector.Filter
		if filterInput == nil {
			filterInput = &minerActionFilterInput{}
		}
		return &minercommandv1.DeviceSelector{
				SelectionType: &minercommandv1.DeviceSelector_AllMatchingFilter{
					AllMatchingFilter: filter,
				},
			}, minerActionSelectionView{
				Type:        minerActionSelectorFilter,
				Description: "miners matching the supplied filter",
				Filter:      filterInput,
			}, nil
	default:
		return nil, minerActionSelectionView{}, fmt.Errorf("invalid %s arguments: unsupported selector.type %q", toolName, input.Selector.Type)
	}
}

func buildMinerActionFilter(input *minerActionFilterInput, toolName string) (*fleetv1.MinerListFilter, error) {
	if input == nil {
		return &fleetv1.MinerListFilter{}, nil
	}
	if err := validatePositiveIDsForTool(toolName, "site_ids", input.SiteIDs); err != nil {
		return nil, err
	}
	if err := validatePositiveIDsForTool(toolName, "building_ids", input.BuildingIDs); err != nil {
		return nil, err
	}
	if err := validatePositiveIDsForTool(toolName, "rack_ids", input.RackIDs); err != nil {
		return nil, err
	}
	statuses := make([]fleetv1.DeviceStatus, 0, len(input.DeviceStatuses))
	for _, status := range input.DeviceStatuses {
		parsed, err := parseDeviceStatusArgument(toolName, status)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, parsed)
	}
	return &fleetv1.MinerListFilter{
		DeviceStatus:      statuses,
		SiteIds:           input.SiteIDs,
		IncludeUnassigned: input.IncludeUnassigned,
		BuildingIds:       input.BuildingIDs,
		IncludeNoBuilding: input.IncludeNoBuilding,
		RackIds:           input.RackIDs,
		IncludeNoRack:     input.IncludeNoRack,
		Models:            input.Models,
		IpCidrs:           input.IPCIDRs,
	}, nil
}

func validateExplicitDeviceIdentifiers(toolName string, identifiers []string, maxItems int) error {
	if len(identifiers) == 0 || len(identifiers) > maxItems {
		return fmt.Errorf("invalid %s arguments: device_identifiers must contain between 1 and %d miners", toolName, maxItems)
	}
	seen := make(map[string]struct{}, len(identifiers))
	for _, identifier := range identifiers {
		if strings.TrimSpace(identifier) == "" || len(identifier) > 255 {
			return fmt.Errorf("invalid %s arguments: device identifiers must contain 1 to 255 characters", toolName)
		}
		if _, exists := seen[identifier]; exists {
			return fmt.Errorf("invalid %s arguments: duplicate device identifier %q", toolName, identifier)
		}
		seen[identifier] = struct{}{}
	}
	return nil
}

func buildDowntimeWindowRequest(arguments json.RawMessage) (downtimeWindowInput, *schedulev1.CreateScheduleRequest, error) {
	var input downtimeWindowInput
	if err := decodeToolArguments(arguments, &input); err != nil {
		return input, nil, fmt.Errorf("invalid downtime window arguments: %w", err)
	}
	action, err := parseScheduleAction(input.Action)
	if err != nil {
		return input, nil, fmt.Errorf("invalid downtime window arguments: %w", err)
	}
	if len(input.Targets) == 0 || len(input.Targets) > maxDowntimeTargets {
		return input, nil, fmt.Errorf("invalid downtime window arguments: targets must contain between 1 and %d items", maxDowntimeTargets)
	}
	targets := make([]*schedulev1.ScheduleTarget, 0, len(input.Targets))
	for i, target := range input.Targets {
		targetType, err := parseScheduleTargetType(target.Type)
		if err != nil {
			return input, nil, fmt.Errorf("invalid downtime window arguments: targets[%d]: %w", i, err)
		}
		targetID := strings.TrimSpace(target.TargetID)
		if targetID == "" {
			return input, nil, fmt.Errorf("invalid downtime window arguments: targets[%d].target_id is required", i)
		}
		targets = append(targets, &schedulev1.ScheduleTarget{TargetType: targetType, TargetId: targetID})
	}
	var actionConfig *schedulev1.PowerTargetConfig
	if action == schedulev1.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET {
		mode, err := parsePowerTargetMode(input.PowerTargetMode)
		if err != nil {
			return input, nil, fmt.Errorf("invalid downtime window arguments: %w", err)
		}
		actionConfig = &schedulev1.PowerTargetConfig{Mode: mode}
	} else if strings.TrimSpace(input.PowerTargetMode) != "" {
		return input, nil, fmt.Errorf("invalid downtime window arguments: power_target_mode is only valid for set_power_target")
	}
	request := &schedulev1.CreateScheduleRequest{
		Name:         input.Name,
		Action:       action,
		ActionConfig: actionConfig,
		ScheduleType: schedulev1.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
		StartDate:    input.StartDate,
		StartTime:    input.StartTime,
		EndDate:      input.EndDate,
		EndTime:      input.EndTime,
		Timezone:     input.Timezone,
		Targets:      targets,
	}
	if err := protovalidate.Validate(request); err != nil {
		return input, nil, fmt.Errorf("invalid downtime window arguments: %w", err)
	}
	return input, request, nil
}

func parseScheduleAction(action string) (schedulev1.ScheduleAction, error) {
	switch normalizeToolEnum(action) {
	case "sleep":
		return schedulev1.ScheduleAction_SCHEDULE_ACTION_SLEEP, nil
	case "reboot":
		return schedulev1.ScheduleAction_SCHEDULE_ACTION_REBOOT, nil
	case "set_power_target":
		return schedulev1.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET, nil
	default:
		return schedulev1.ScheduleAction_SCHEDULE_ACTION_UNSPECIFIED, fmt.Errorf("unsupported action %q", action)
	}
}

func parsePowerTargetMode(mode string) (schedulev1.PowerTargetMode, error) {
	switch normalizeToolEnum(mode) {
	case "default":
		return schedulev1.PowerTargetMode_POWER_TARGET_MODE_DEFAULT, nil
	case "max":
		return schedulev1.PowerTargetMode_POWER_TARGET_MODE_MAX, nil
	default:
		return schedulev1.PowerTargetMode_POWER_TARGET_MODE_UNSPECIFIED, fmt.Errorf("power_target_mode must be default or max")
	}
}

func parseScheduleTargetType(targetType string) (schedulev1.ScheduleTargetType, error) {
	switch normalizeToolEnum(targetType) {
	case "rack":
		return schedulev1.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK, nil
	case "miner":
		return schedulev1.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, nil
	case "group":
		return schedulev1.ScheduleTargetType_SCHEDULE_TARGET_TYPE_GROUP, nil
	case "site":
		return schedulev1.ScheduleTargetType_SCHEDULE_TARGET_TYPE_SITE, nil
	case "building":
		return schedulev1.ScheduleTargetType_SCHEDULE_TARGET_TYPE_BUILDING, nil
	default:
		return schedulev1.ScheduleTargetType_SCHEDULE_TARGET_TYPE_UNSPECIFIED, fmt.Errorf("unsupported target type %q", targetType)
	}
}

func normalizeToolEnum(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	return normalized
}

func buildSetRackSlotsInput(arguments json.RawMessage) (setRackSlotsInput, error) {
	var input setRackSlotsInput
	if err := decodeToolArguments(arguments, &input); err != nil {
		return input, fmt.Errorf("invalid set_rack_slots arguments: %w", err)
	}
	if input.RackID <= 0 {
		return input, fmt.Errorf("invalid set_rack_slots arguments: rack_id must be positive")
	}
	if len(input.SlotAssignments) == 0 || len(input.SlotAssignments) > maxRackSlotAssignments {
		return input, fmt.Errorf("invalid set_rack_slots arguments: slot_assignments must contain between 1 and %d assignments", maxRackSlotAssignments)
	}
	seenDevices := make(map[string]struct{}, len(input.SlotAssignments))
	seenPositions := make(map[string]string, len(input.SlotAssignments))
	for i, assignment := range input.SlotAssignments {
		if strings.TrimSpace(assignment.DeviceIdentifier) == "" || len(assignment.DeviceIdentifier) > 255 {
			return input, fmt.Errorf("invalid set_rack_slots arguments: slot_assignments[%d].device_identifier must contain 1 to 255 characters", i)
		}
		if _, exists := seenDevices[assignment.DeviceIdentifier]; exists {
			return input, fmt.Errorf("invalid set_rack_slots arguments: duplicate device identifier %q", assignment.DeviceIdentifier)
		}
		seenDevices[assignment.DeviceIdentifier] = struct{}{}
		if assignment.Row < 0 || assignment.Column < 0 {
			return input, fmt.Errorf("invalid set_rack_slots arguments: row and column must be 0-indexed non-negative coordinates")
		}
		positionKey := rackSlotPositionKey(assignment.Row, assignment.Column)
		if existing, exists := seenPositions[positionKey]; exists {
			return input, fmt.Errorf("invalid set_rack_slots arguments: devices %q and %q target the same slot (%d,%d)", existing, assignment.DeviceIdentifier, assignment.Row, assignment.Column)
		}
		seenPositions[positionKey] = assignment.DeviceIdentifier
	}
	return input, nil
}

func buildClearRackSlotsInput(arguments json.RawMessage) (clearRackSlotsInput, error) {
	var input clearRackSlotsInput
	if err := decodeToolArguments(arguments, &input); err != nil {
		return input, fmt.Errorf("invalid clear_rack_slots arguments: %w", err)
	}
	if input.RackID <= 0 {
		return input, fmt.Errorf("invalid clear_rack_slots arguments: rack_id must be positive")
	}
	if len(input.DeviceIdentifiers) == 0 || len(input.DeviceIdentifiers) > maxRackSlotAssignments {
		return input, fmt.Errorf("invalid clear_rack_slots arguments: device_identifiers must contain between 1 and %d miners", maxRackSlotAssignments)
	}
	seen := make(map[string]struct{}, len(input.DeviceIdentifiers))
	for _, identifier := range input.DeviceIdentifiers {
		if strings.TrimSpace(identifier) == "" || len(identifier) > 255 {
			return input, fmt.Errorf("invalid clear_rack_slots arguments: device identifiers must contain 1 to 255 characters")
		}
		if _, exists := seen[identifier]; exists {
			return input, fmt.Errorf("invalid clear_rack_slots arguments: duplicate device identifier %q", identifier)
		}
		seen[identifier] = struct{}{}
	}
	return input, nil
}

func (t *FleetTools) Confirmation(name string, arguments json.RawMessage) (*chatdomain.ToolConfirmation, error) {
	switch name {
	case "create_site":
		input, _, err := buildCreateSiteRequest(arguments)
		if err != nil {
			return nil, err
		}
		details := []chatdomain.ToolConfirmationDetail{{Label: "Name", Value: input.Name}}
		locationParts := make([]string, 0, 3)
		for _, part := range []string{input.Address, input.LocationCity, input.LocationState, input.PostalCode, input.Country} {
			if strings.TrimSpace(part) != "" {
				locationParts = append(locationParts, part)
			}
		}
		location := strings.Join(locationParts, ", ")
		if location != "" {
			details = append(details, chatdomain.ToolConfirmationDetail{Label: "Location", Value: location})
		}
		if input.PowerCapacityMW > 0 {
			details = append(details, chatdomain.ToolConfirmationDetail{Label: "Power capacity", Value: fmt.Sprintf("%g MW", input.PowerCapacityMW)})
		}
		if input.Timezone != "" {
			details = append(details, chatdomain.ToolConfirmationDetail{Label: "Timezone", Value: input.Timezone})
		}
		if input.Notes != "" {
			details = append(details, chatdomain.ToolConfirmationDetail{Label: "Notes", Value: input.Notes})
		}
		return &chatdomain.ToolConfirmation{
			Title:        "Create this site?",
			Description:  "Proto AI will add a new site to your fleet.",
			ConfirmLabel: "Create site",
			Details:      details,
		}, nil
	case "create_rack":
		input, _, err := buildCreateRackRequest(arguments)
		if err != nil {
			return nil, err
		}
		details := []chatdomain.ToolConfirmationDetail{
			{Label: "Rack", Value: input.Label},
			{Label: "Layout", Value: fmt.Sprintf("%d rows × %d columns", input.Rows, input.Columns)},
		}
		if input.Zone != "" {
			details = append(details, chatdomain.ToolConfirmationDetail{Label: "Zone", Value: input.Zone})
		}
		orderIndex := input.OrderIndex
		if orderIndex == "" {
			orderIndex = rackOrderIndexTopLeft
		}
		coolingType := input.CoolingType
		if coolingType == "" {
			coolingType = "air"
		}
		details = append(details,
			chatdomain.ToolConfirmationDetail{Label: "Numbering starts", Value: strings.ReplaceAll(orderIndex, "_", " ")},
			chatdomain.ToolConfirmationDetail{Label: "Cooling", Value: coolingType},
		)
		if input.BuildingID != nil {
			details = append(details, chatdomain.ToolConfirmationDetail{Label: "Building ID", Value: fmt.Sprint(*input.BuildingID)})
		} else if input.SiteID != nil {
			details = append(details, chatdomain.ToolConfirmationDetail{Label: "Site ID", Value: fmt.Sprint(*input.SiteID)})
		} else {
			details = append(details, chatdomain.ToolConfirmationDetail{Label: "Placement", Value: "Unassigned"})
		}
		return &chatdomain.ToolConfirmation{
			Title:        "Create this rack?",
			Description:  "Proto AI will create an empty rack with this layout and placement.",
			ConfirmLabel: "Create rack",
			Details:      details,
		}, nil
	case "move_miners_to_rack":
		input, _, err := buildMoveMinersRequest(arguments)
		if err != nil {
			return nil, err
		}
		description := "Proto AI will atomically replace the rack membership for these miners."
		if input.ForceClearConflictingSite {
			description += " Miners assigned to a site may have their site and building cleared if the destination rack is unassigned."
		}
		return &chatdomain.ToolConfirmation{
			Title:        fmt.Sprintf("Move %d miner(s)?", len(input.DeviceIdentifiers)),
			Description:  description,
			ConfirmLabel: "Move miners",
			Details: []chatdomain.ToolConfirmationDetail{
				{Label: "Destination rack ID", Value: fmt.Sprint(input.TargetRackID)},
				{Label: "Miners", Value: strings.Join(input.DeviceIdentifiers, ", ")},
			},
		}, nil
	case "set_rack_slots":
		input, err := buildSetRackSlotsInput(arguments)
		if err != nil {
			return nil, err
		}
		return &chatdomain.ToolConfirmation{
			Title:        fmt.Sprintf("Assign %d rack slot(s)?", len(input.SlotAssignments)),
			Description:  "Proto AI will update slot positions for miners already assigned to this rack. Rack membership and rack placement are preserved.",
			ConfirmLabel: "Assign slots",
			Details: []chatdomain.ToolConfirmationDetail{
				{Label: "Rack ID", Value: fmt.Sprint(input.RackID)},
				{Label: "Slots", Value: formatRackSlotAssignments(input.SlotAssignments, 20)},
			},
		}, nil
	case "clear_rack_slots":
		input, err := buildClearRackSlotsInput(arguments)
		if err != nil {
			return nil, err
		}
		return &chatdomain.ToolConfirmation{
			Title:        fmt.Sprintf("Clear %d rack slot(s)?", len(input.DeviceIdentifiers)),
			Description:  "Proto AI will clear slot positions for these miners while preserving rack membership.",
			ConfirmLabel: "Clear slots",
			Details: []chatdomain.ToolConfirmationDetail{
				{Label: "Rack ID", Value: fmt.Sprint(input.RackID)},
				{Label: "Miners", Value: formatStringList(input.DeviceIdentifiers, 20)},
			},
		}, nil
	case "execute_miner_action":
		input, _, _, selection, err := buildMinerActionInput(arguments, "execute_miner_action")
		if err != nil {
			return nil, err
		}
		actionLabel := strings.ReplaceAll(normalizeToolEnum(input.Action), "_", " ")
		return &chatdomain.ToolConfirmation{
			Title:        fmt.Sprintf("Execute %s on %s?", actionLabel, minerActionTargetPhrase(selection)),
			Description:  "Proto AI will submit this command through the existing miner command service.",
			ConfirmLabel: "Execute command",
			Details: []chatdomain.ToolConfirmationDetail{
				{Label: "Action", Value: actionLabel},
				{Label: "Selection", Value: minerActionSelectionDetail(selection)},
			},
		}, nil
	case "create_downtime_window":
		input, request, err := buildDowntimeWindowRequest(arguments)
		if err != nil {
			return nil, err
		}
		details := []chatdomain.ToolConfirmationDetail{
			{Label: "Name", Value: input.Name},
			{Label: "Action", Value: scheduleActionLabel(request.GetAction())},
			{Label: "Starts", Value: fmt.Sprintf("%s %s %s", input.StartDate, input.StartTime, input.Timezone)},
			{Label: "Targets", Value: formatDowntimeTargets(input.Targets, 20)},
		}
		if input.EndTime != "" {
			endDate := input.EndDate
			if endDate == "" {
				endDate = input.StartDate
			}
			details = append(details, chatdomain.ToolConfirmationDetail{Label: "Ends", Value: fmt.Sprintf("%s %s %s", endDate, input.EndTime, input.Timezone)})
		}
		if request.GetAction() == schedulev1.ScheduleAction_SCHEDULE_ACTION_SLEEP {
			details = append(details, chatdomain.ToolConfirmationDetail{Label: "Resume", Value: "Not automatic for sleep schedules"})
		}
		return &chatdomain.ToolConfirmation{
			Title:        fmt.Sprintf("Create schedule %q?", input.Name),
			Description:  "Proto AI will create this one-time maintenance schedule through the existing schedule service.",
			ConfirmLabel: "Create schedule",
			Details:      details,
		}, nil
	default:
		return nil, nil
	}
}

func (t *FleetTools) Execute(ctx context.Context, name string, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	switch name {
	case "get_miner_state_counts":
		return t.getMinerStateCounts(ctx, arguments)
	case "resolve_miners":
		return t.resolveMiners(ctx, arguments)
	case "list_sites":
		return t.listSites(ctx)
	case "get_site_health_summary":
		return t.getSiteHealthSummary(ctx, arguments)
	case "list_pools":
		return t.listPools(ctx)
	case "list_actionable_miner_issues":
		return t.listActionableMinerIssues(ctx, arguments)
	case "list_racks":
		return t.listRacks(ctx)
	case "get_rack_slots":
		return t.getRackSlots(ctx, arguments)
	case "get_rack_health":
		return t.getRackHealth(ctx, arguments)
	case "preview_miner_action":
		return t.previewMinerAction(ctx, arguments)
	case "execute_miner_action":
		return t.executeMinerAction(ctx, arguments)
	case "preview_downtime_window":
		return t.previewDowntimeWindow(ctx, arguments)
	case "create_downtime_window":
		return t.createDowntimeWindow(ctx, arguments)
	case "create_site":
		return t.createSite(ctx, arguments)
	case "create_rack":
		return t.createRack(ctx, arguments)
	case "move_miners_to_rack":
		return t.moveMinersToRack(ctx, arguments)
	case "set_rack_slots":
		return t.setRackSlots(ctx, arguments)
	case "clear_rack_slots":
		return t.clearRackSlots(ctx, arguments)
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

func (t *FleetTools) getSiteHealthSummary(ctx context.Context, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	var input struct {
		SiteID            *int64 `json:"site_id"`
		IncludeUnassigned bool   `json:"include_unassigned"`
	}
	if err := decodeToolArguments(arguments, &input); err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("invalid get_site_health_summary arguments: %w", err)
	}
	siteIDs := []int64(nil)
	if input.SiteID != nil {
		if *input.SiteID <= 0 {
			return chatdomain.ToolOutput{}, fmt.Errorf("invalid get_site_health_summary arguments: site_id must be positive")
		}
		siteIDs = []int64{*input.SiteID}
	}
	response, err := t.fleet.GetMinerStateCounts(ctx, connect.NewRequest(&fleetv1.GetMinerStateCountsRequest{
		SiteIds:           siteIDs,
		IncludeUnassigned: input.IncludeUnassigned,
	}))
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	counts := response.Msg.GetStateCounts()

	siteInventory, err := t.siteInventory(ctx, input.SiteID)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	payload := map[string]any{
		"scope": map[string]any{
			"site_id":            input.SiteID,
			"include_unassigned": input.IncludeUnassigned,
		},
		"total_miners": response.Msg.GetTotalMiners(),
		"state_counts": map[string]any{
			"hashing":  counts.GetHashingCount(),
			"broken":   counts.GetBrokenCount(),
			"offline":  counts.GetOfflineCount(),
			"sleeping": counts.GetSleepingCount(),
		},
		"sites": siteInventory,
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal site health summary: %w", err)
	}
	return chatdomain.ToolOutput{
		Content: string(content),
		Summary: fmt.Sprintf("Read health summary for %d miner(s)", response.Msg.GetTotalMiners()),
	}, nil
}

type resourceRefView struct {
	ID    int64  `json:"id"`
	Label string `json:"label"`
}

type resolvedMinerView struct {
	DeviceIdentifier string           `json:"device_identifier"`
	Name             string           `json:"name,omitempty"`
	Status           string           `json:"status"`
	Model            string           `json:"model,omitempty"`
	IPAddress        string           `json:"ip_address,omitempty"`
	Site             *resourceRefView `json:"site,omitempty"`
	Building         *resourceRefView `json:"building,omitempty"`
	Rack             *resourceRefView `json:"rack,omitempty"`
	Zone             string           `json:"zone,omitempty"`
}

func (t *FleetTools) listMatchingMinerSnapshots(
	ctx context.Context,
	input resolveMinersInput,
	filter *fleetv1.MinerListFilter,
) ([]*fleetv1.MinerStateSnapshot, int, int32, bool, error) {
	pageSize := input.Limit
	if input.Query != "" {
		pageSize = maxResolveMinersLimit
	}
	cursor := ""
	scanned := 0
	matchedScanned := 0
	totalAvailable := int32(0)
	totalAvailableSet := false
	truncated := false
	miners := make([]*fleetv1.MinerStateSnapshot, 0, min(int(input.Limit), defaultResolveMinersLimit))

	for {
		response, err := t.fleet.ListMinerStateSnapshots(ctx, connect.NewRequest(&fleetv1.ListMinerStateSnapshotsRequest{
			PageSize: pageSize,
			Cursor:   cursor,
			Filter:   filter,
		}))
		if err != nil {
			return nil, 0, 0, false, err
		}
		if !totalAvailableSet {
			totalAvailable = response.Msg.GetTotalMiners()
			totalAvailableSet = true
		}

		for _, miner := range response.Msg.GetMiners() {
			scanned++
			if !matchesResolveMinerQuery(miner, input.Query) {
				continue
			}
			matchedScanned++
			if len(miners) >= int(input.Limit) {
				truncated = true
				continue
			}
			miners = append(miners, miner)
		}

		nextCursor := response.Msg.GetCursor()
		if nextCursor == "" {
			break
		}
		if input.Query == "" || len(miners) >= int(input.Limit) || scanned >= maxResolveMinersScan {
			truncated = true
			break
		}
		cursor = nextCursor
	}

	return miners, matchedScanned, totalAvailable, truncated, nil
}

func (t *FleetTools) resolveMiners(ctx context.Context, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	input, filter, err := buildResolveMinersFilter(arguments)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	snapshots, matchedScanned, totalAvailable, truncated, err := t.listMatchingMinerSnapshots(ctx, input, filter)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}

	miners := make([]resolvedMinerView, 0, len(snapshots))
	deviceIdentifiers := make([]string, 0, len(snapshots))
	for _, miner := range snapshots {
		miners = append(miners, resolvedMinerViewFromSnapshot(miner))
		deviceIdentifiers = append(deviceIdentifiers, miner.GetDeviceIdentifier())
	}
	payload := map[string]any{
		"device_identifiers": deviceIdentifiers,
		"miners":             miners,
		"returned":           len(miners),
		"matched_scanned":    matchedScanned,
		"total_available":    totalAvailable,
		"truncated":          truncated,
	}
	if input.Query != "" {
		payload["query"] = input.Query
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal resolved miners: %w", err)
	}
	summary := fmt.Sprintf("Resolved %d miner(s)", len(miners))
	if truncated {
		summary += "; more matches may exist"
	}
	return chatdomain.ToolOutput{Content: string(content), Summary: summary}, nil
}

type actionableMinerIssueView struct {
	DeviceIdentifier string           `json:"device_identifier"`
	Name             string           `json:"name,omitempty"`
	Status           string           `json:"status"`
	Severity         string           `json:"severity"`
	Issue            string           `json:"issue"`
	SuggestedAction  string           `json:"suggested_action"`
	Model            string           `json:"model,omitempty"`
	IPAddress        string           `json:"ip_address,omitempty"`
	Site             *resourceRefView `json:"site,omitempty"`
	Building         *resourceRefView `json:"building,omitempty"`
	Rack             *resourceRefView `json:"rack,omitempty"`
	Zone             string           `json:"zone,omitempty"`
}

func (t *FleetTools) listActionableMinerIssues(ctx context.Context, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	input, filter, err := buildResolveMinersFilter(arguments)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	if len(filter.GetDeviceStatus()) == 0 {
		filter.DeviceStatus = []fleetv1.DeviceStatus{
			fleetv1.DeviceStatus_DEVICE_STATUS_OFFLINE,
			fleetv1.DeviceStatus_DEVICE_STATUS_ERROR,
			fleetv1.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL,
			fleetv1.DeviceStatus_DEVICE_STATUS_REBOOT_REQUIRED,
		}
	}
	snapshots, matchedScanned, totalAvailable, truncated, err := t.listMatchingMinerSnapshots(ctx, input, filter)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	issues := make([]actionableMinerIssueView, 0, len(snapshots))
	for _, miner := range snapshots {
		issue, include := actionableIssueFromMiner(miner)
		if include {
			issues = append(issues, issue)
		}
	}
	payload := map[string]any{
		"issues":          issues,
		"returned":        len(issues),
		"matched_scanned": matchedScanned,
		"total_available": totalAvailable,
		"truncated":       truncated,
	}
	if input.Query != "" {
		payload["query"] = input.Query
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal actionable miner issues: %w", err)
	}
	summary := fmt.Sprintf("Found %d actionable miner issue(s)", len(issues))
	if truncated {
		summary += "; more matches may exist"
	}
	return chatdomain.ToolOutput{Content: string(content), Summary: summary}, nil
}

func actionableIssueFromMiner(miner *fleetv1.MinerStateSnapshot) (actionableMinerIssueView, bool) {
	severity, issue, suggestedAction, include := actionableStatus(miner.GetDeviceStatus())
	if !include {
		return actionableMinerIssueView{}, false
	}
	base := resolvedMinerViewFromSnapshot(miner)
	return actionableMinerIssueView{
		DeviceIdentifier: base.DeviceIdentifier,
		Name:             base.Name,
		Status:           base.Status,
		Severity:         severity,
		Issue:            issue,
		SuggestedAction:  suggestedAction,
		Model:            base.Model,
		IPAddress:        base.IPAddress,
		Site:             base.Site,
		Building:         base.Building,
		Rack:             base.Rack,
		Zone:             base.Zone,
	}, true
}

func actionableStatus(status fleetv1.DeviceStatus) (string, string, string, bool) {
	switch status {
	case fleetv1.DeviceStatus_DEVICE_STATUS_OFFLINE:
		return "high", "Miner is offline", "Check power and network reachability; if reachable, preview a reboot before executing it.", true
	case fleetv1.DeviceStatus_DEVICE_STATUS_ERROR:
		return "high", "Miner is in an error state", "Inspect device errors and recent activity before deciding whether to reboot or repair.", true
	case fleetv1.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL:
		return "medium", "Miner is missing a mining pool", "Assign an approved mining pool from the normal pool management flow.", true
	case fleetv1.DeviceStatus_DEVICE_STATUS_REBOOT_REQUIRED:
		return "medium", "Miner reports reboot required", "Use preview_miner_action with reboot, then execute_miner_action after operator confirmation.", true
	case fleetv1.DeviceStatus_DEVICE_STATUS_UPDATING:
		return "medium", "Miner is updating", "Monitor update progress before issuing another command.", true
	case fleetv1.DeviceStatus_DEVICE_STATUS_MAINTENANCE:
		return "low", "Miner is in maintenance", "Confirm maintenance intent before returning it to service.", true
	case fleetv1.DeviceStatus_DEVICE_STATUS_INACTIVE:
		return "low", "Miner is inactive or sleeping", "If this miner should be hashing, preview start_mining before executing it.", true
	case fleetv1.DeviceStatus_DEVICE_STATUS_UNSPECIFIED, fleetv1.DeviceStatus_DEVICE_STATUS_ONLINE:
		return "", "", "", false
	default:
		return "", "", "", false
	}
}

func matchesResolveMinerQuery(miner *fleetv1.MinerStateSnapshot, query string) bool {
	if query == "" {
		return true
	}
	needle := strings.ToLower(query)
	fields := []string{
		miner.GetDeviceIdentifier(),
		miner.GetName(),
		miner.GetMacAddress(),
		miner.GetSerialNumber(),
		miner.GetIpAddress(),
		miner.GetModel(),
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), needle) {
			return true
		}
	}
	return false
}

func resolvedMinerViewFromSnapshot(miner *fleetv1.MinerStateSnapshot) resolvedMinerView {
	view := resolvedMinerView{
		DeviceIdentifier: miner.GetDeviceIdentifier(),
		Name:             miner.GetName(),
		Status:           deviceStatusLabel(miner.GetDeviceStatus()),
		Model:            miner.GetModel(),
		IPAddress:        miner.GetIpAddress(),
	}
	if placement := miner.GetPlacement(); placement != nil {
		view.Site = resourceRefViewFromProto(placement.GetSite())
		view.Building = resourceRefViewFromProto(placement.GetBuilding())
		view.Rack = resourceRefViewFromProto(placement.GetRack())
		view.Zone = placement.GetZone()
	}
	return view
}

func resourceRefViewFromProto(ref *commonv1.ResourceRef) *resourceRefView {
	if ref == nil {
		return nil
	}
	return &resourceRefView{ID: ref.GetId(), Label: ref.GetLabel()}
}

func deviceStatusLabel(status fleetv1.DeviceStatus) string {
	label := strings.TrimPrefix(status.String(), "DEVICE_STATUS_")
	return strings.ToLower(label)
}

func rackOrderIndexLabel(orderIndex devicesetv1.RackOrderIndex) string {
	switch orderIndex {
	case devicesetv1.RackOrderIndex_RACK_ORDER_INDEX_UNSPECIFIED:
		return labelUnspecified
	case devicesetv1.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT:
		return rackOrderIndexBottomLeft
	case devicesetv1.RackOrderIndex_RACK_ORDER_INDEX_TOP_LEFT:
		return rackOrderIndexTopLeft
	case devicesetv1.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_RIGHT:
		return rackOrderIndexBottomRight
	case devicesetv1.RackOrderIndex_RACK_ORDER_INDEX_TOP_RIGHT:
		return rackOrderIndexTopRight
	default:
		return labelUnspecified
	}
}

func formatRackSlotAssignments(assignments []rackSlotAssignmentInput, limit int) string {
	parts := make([]string, 0, min(len(assignments), limit)+1)
	for i, assignment := range assignments {
		if i >= limit {
			parts = append(parts, fmt.Sprintf("+%d more", len(assignments)-limit))
			break
		}
		parts = append(parts, fmt.Sprintf("%s → row %d, column %d", assignment.DeviceIdentifier, assignment.Row, assignment.Column))
	}
	return strings.Join(parts, "; ")
}

func formatStringList(values []string, limit int) string {
	if len(values) <= limit {
		return strings.Join(values, ", ")
	}
	parts := append([]string{}, values[:limit]...)
	parts = append(parts, fmt.Sprintf("+%d more", len(values)-limit))
	return strings.Join(parts, ", ")
}

func minerActionTargetPhrase(selection minerActionSelectionView) string {
	switch selection.Type {
	case minerActionSelectorExplicit:
		return fmt.Sprintf("%d miner(s)", len(selection.DeviceIdentifiers))
	case minerActionSelectorAllDevices:
		return "the whole fleet"
	case minerActionSelectorFilter:
		return "matching miners"
	default:
		return selection.Description
	}
}

func minerActionSelectionDetail(selection minerActionSelectionView) string {
	switch selection.Type {
	case minerActionSelectorExplicit:
		return formatStringList(selection.DeviceIdentifiers, 20)
	case minerActionSelectorAllDevices:
		return "Whole fleet command-eligible miners"
	case minerActionSelectorFilter:
		return formatMinerActionFilter(selection.Filter)
	default:
		return selection.Description
	}
}

func formatMinerActionFilter(filter *minerActionFilterInput) string {
	if filter == nil {
		return "All command-eligible miners matching an empty filter"
	}
	parts := make([]string, 0, 8)
	if len(filter.DeviceStatuses) > 0 {
		parts = append(parts, "statuses: "+strings.Join(filter.DeviceStatuses, ", "))
	}
	if len(filter.SiteIDs) > 0 {
		parts = append(parts, "site IDs: "+formatInt64List(filter.SiteIDs, 20))
	}
	if filter.IncludeUnassigned {
		parts = append(parts, "include unassigned")
	}
	if len(filter.BuildingIDs) > 0 {
		parts = append(parts, "building IDs: "+formatInt64List(filter.BuildingIDs, 20))
	}
	if filter.IncludeNoBuilding {
		parts = append(parts, "include no building")
	}
	if len(filter.RackIDs) > 0 {
		parts = append(parts, "rack IDs: "+formatInt64List(filter.RackIDs, 20))
	}
	if filter.IncludeNoRack {
		parts = append(parts, "include no rack")
	}
	if len(filter.Models) > 0 {
		parts = append(parts, "models: "+formatStringList(filter.Models, 20))
	}
	if len(filter.IPCIDRs) > 0 {
		parts = append(parts, "IP CIDRs: "+formatStringList(filter.IPCIDRs, 20))
	}
	if len(parts) == 0 {
		return "All command-eligible miners matching an empty filter"
	}
	return strings.Join(parts, "; ")
}

func formatInt64List(values []int64, limit int) string {
	parts := make([]string, 0, min(len(values), limit)+1)
	for i, value := range values {
		if i >= limit {
			parts = append(parts, fmt.Sprintf("+%d more", len(values)-limit))
			break
		}
		parts = append(parts, fmt.Sprint(value))
	}
	return strings.Join(parts, ", ")
}

type siteInventoryView struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	DeviceCount    int64  `json:"device_count"`
	BuildingCount  int64  `json:"building_count"`
	RackCount      int64  `json:"rack_count"`
	Infrastructure int64  `json:"infrastructure_device_count"`
}

func (t *FleetTools) siteInventory(ctx context.Context, siteID *int64) ([]siteInventoryView, error) {
	response, err := t.sites.ListSites(ctx, connect.NewRequest(&sitesv1.ListSitesRequest{}))
	if err != nil {
		return nil, err
	}
	sites := make([]siteInventoryView, 0, len(response.Msg.GetSites()))
	for _, item := range response.Msg.GetSites() {
		if siteID != nil && item.GetSite().GetId() != *siteID {
			continue
		}
		sites = append(sites, siteInventoryView{
			ID:             item.GetSite().GetId(),
			Name:           item.GetSite().GetName(),
			DeviceCount:    item.GetDeviceCount(),
			BuildingCount:  item.GetBuildingCount(),
			RackCount:      item.GetRackCount(),
			Infrastructure: item.GetInfrastructureDeviceCount(),
		})
	}
	return sites, nil
}

func (t *FleetTools) listSites(ctx context.Context) (chatdomain.ToolOutput, error) {
	sites, err := t.siteInventory(ctx, nil)
	if err != nil {
		return chatdomain.ToolOutput{}, err
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

func (t *FleetTools) listRacks(ctx context.Context) (chatdomain.ToolOutput, error) {
	response, err := t.deviceSets.ListDeviceSets(ctx, connect.NewRequest(&devicesetv1.ListDeviceSetsRequest{
		Type:     devicesetv1.DeviceSetType_DEVICE_SET_TYPE_RACK,
		PageSize: 1000,
	}))
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	type rackView struct {
		ID              int64  `json:"id"`
		Label           string `json:"label"`
		Rows            int32  `json:"rows"`
		Columns         int32  `json:"columns"`
		NumberingOrigin string `json:"numbering_origin"`
		DeviceCount     int32  `json:"device_count"`
		Site            string `json:"site,omitempty"`
		Building        string `json:"building,omitempty"`
		Zone            string `json:"zone,omitempty"`
	}
	racks := make([]rackView, 0, len(response.Msg.GetDeviceSets()))
	for _, rack := range response.Msg.GetDeviceSets() {
		rackInfo := rack.GetRackInfo()
		view := rackView{
			ID:              rack.GetId(),
			Label:           rack.GetLabel(),
			Rows:            rackInfo.GetRows(),
			Columns:         rackInfo.GetColumns(),
			NumberingOrigin: rackOrderIndexLabel(rackInfo.GetOrderIndex()),
			DeviceCount:     rack.GetDeviceCount(),
			Zone:            rackInfo.GetZone(),
		}
		if placement := rack.GetPlacement(); placement != nil {
			view.Site = placement.GetSite().GetLabel()
			view.Building = placement.GetBuilding().GetLabel()
		}
		racks = append(racks, view)
	}
	content, err := json.Marshal(map[string]any{"racks": racks})
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal racks: %w", err)
	}
	return chatdomain.ToolOutput{Content: string(content), Summary: fmt.Sprintf("Read %d racks", len(racks))}, nil
}

type rackSlotView struct {
	DeviceIdentifier string `json:"device_identifier"`
	Row              int32  `json:"row"`
	Column           int32  `json:"column"`
}

func rackSlotViewFromProto(slot *devicesetv1.RackSlot) rackSlotView {
	return rackSlotView{
		DeviceIdentifier: slot.GetDeviceIdentifier(),
		Row:              slot.GetPosition().GetRow(),
		Column:           slot.GetPosition().GetColumn(),
	}
}

func rackSlotAssignmentView(assignment rackSlotAssignmentInput) rackSlotView {
	return rackSlotView{
		DeviceIdentifier: assignment.DeviceIdentifier,
		Row:              assignment.Row,
		Column:           assignment.Column,
	}
}

func (t *FleetTools) getRackSlots(ctx context.Context, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	rackID, request, err := buildGetRackSlotsRequest(arguments)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	response, err := t.deviceSets.GetRackSlots(ctx, connect.NewRequest(request))
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	slots := make([]rackSlotView, 0, len(response.Msg.GetSlots()))
	for _, slot := range response.Msg.GetSlots() {
		slots = append(slots, rackSlotViewFromProto(slot))
	}
	content, err := json.Marshal(map[string]any{
		"rack_id":        rackID,
		"occupied_count": len(slots),
		"occupied_slots": slots,
	})
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal rack slots: %w", err)
	}
	return chatdomain.ToolOutput{
		Content: string(content),
		Summary: fmt.Sprintf("Read %d occupied slot(s) for rack %d", len(slots), rackID),
	}, nil
}

type rackSlotPositionView struct {
	Row    int32 `json:"row"`
	Column int32 `json:"column"`
}

type rackHealthMinerView struct {
	resolvedMinerView
	Slot *rackSlotPositionView `json:"slot,omitempty"`
}

func (t *FleetTools) getRackHealth(ctx context.Context, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	input, err := buildRackHealthInput(arguments)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	rack, members, slots, err := t.loadRackSlotState(ctx, input.RackID)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	resolveInput := resolveMinersInput{
		RackIDs: []int64{input.RackID},
		Limit:   input.Limit,
	}
	filter := &fleetv1.MinerListFilter{RackIds: []int64{input.RackID}}
	snapshots, _, totalAvailable, truncated, err := t.listMatchingMinerSnapshots(ctx, resolveInput, filter)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	slotsByMiner := make(map[string]rackSlotPositionView, len(slots))
	for _, slot := range slots {
		slotsByMiner[slot.GetDeviceIdentifier()] = rackSlotPositionView{
			Row:    slot.GetPosition().GetRow(),
			Column: slot.GetPosition().GetColumn(),
		}
	}
	statusCounts := emptyHealthStatusCounts()
	miners := make([]rackHealthMinerView, 0, len(snapshots))
	for _, miner := range snapshots {
		statusCounts[healthStatusKey(miner.GetDeviceStatus())]++
		view := rackHealthMinerView{resolvedMinerView: resolvedMinerViewFromSnapshot(miner)}
		if slot, ok := slotsByMiner[miner.GetDeviceIdentifier()]; ok {
			view.Slot = &slot
		}
		miners = append(miners, view)
	}
	rackInfo := rack.GetRackInfo()
	payload := map[string]any{
		"rack": map[string]any{
			"id":               rack.GetId(),
			"label":            rack.GetLabel(),
			"rows":             rackInfo.GetRows(),
			"columns":          rackInfo.GetColumns(),
			"numbering_origin": rackOrderIndexLabel(rackInfo.GetOrderIndex()),
			"zone":             rackInfo.GetZone(),
			"device_count":     rack.GetDeviceCount(),
		},
		"member_count":        len(members),
		"occupied_slot_count": len(slots),
		"status_counts":       statusCounts,
		"miners":              miners,
		"returned":            len(miners),
		"total_available":     totalAvailable,
		"truncated":           truncated,
	}
	if placement := rack.GetPlacement(); placement != nil {
		payload["placement"] = map[string]any{
			"site":     resourceRefViewFromProto(placement.GetSite()),
			"building": resourceRefViewFromProto(placement.GetBuilding()),
		}
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal rack health: %w", err)
	}
	return chatdomain.ToolOutput{
		Content: string(content),
		Summary: fmt.Sprintf("Read health for rack %d with %d member(s)", input.RackID, len(members)),
	}, nil
}

func emptyHealthStatusCounts() map[string]int {
	return map[string]int{
		"hashing":            0,
		"broken":             0,
		"offline":            0,
		"sleeping":           0,
		"maintenance":        0,
		"needs_mining_pool":  0,
		"updating":           0,
		"reboot_required":    0,
		"status_unspecified": 0,
	}
}

func healthStatusKey(status fleetv1.DeviceStatus) string {
	switch status {
	case fleetv1.DeviceStatus_DEVICE_STATUS_ONLINE:
		return "hashing"
	case fleetv1.DeviceStatus_DEVICE_STATUS_ERROR:
		return "broken"
	case fleetv1.DeviceStatus_DEVICE_STATUS_INACTIVE:
		return "sleeping"
	case fleetv1.DeviceStatus_DEVICE_STATUS_OFFLINE:
		return "offline"
	case fleetv1.DeviceStatus_DEVICE_STATUS_MAINTENANCE:
		return "maintenance"
	case fleetv1.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL:
		return "needs_mining_pool"
	case fleetv1.DeviceStatus_DEVICE_STATUS_UPDATING:
		return "updating"
	case fleetv1.DeviceStatus_DEVICE_STATUS_REBOOT_REQUIRED:
		return "reboot_required"
	case fleetv1.DeviceStatus_DEVICE_STATUS_UNSPECIFIED:
		return "status_unspecified"
	default:
		return "status_unspecified"
	}
}

func (t *FleetTools) setRackSlots(ctx context.Context, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	input, err := buildSetRackSlotsInput(arguments)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	rack, members, existingSlots, err := t.loadRackSlotState(ctx, input.RackID)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	if err := validateSetRackSlots(input, rack, members, existingSlots); err != nil {
		return chatdomain.ToolOutput{}, err
	}

	requestedDevices := make(map[string]struct{}, len(input.SlotAssignments))
	for _, assignment := range input.SlotAssignments {
		requestedDevices[assignment.DeviceIdentifier] = struct{}{}
	}
	assignmentViews := make([]rackSlotView, 0, len(input.SlotAssignments))
	for _, assignment := range input.SlotAssignments {
		assignmentViews = append(assignmentViews, rackSlotAssignmentView(assignment))
	}

	for identifier := range requestedDevices {
		if _, err := t.deviceSets.ClearRackSlotPosition(ctx, connect.NewRequest(&devicesetv1.ClearRackSlotPositionRequest{
			DeviceSetId:      input.RackID,
			DeviceIdentifier: identifier,
		})); err != nil {
			return chatdomain.ToolOutput{}, err
		}
	}
	for _, assignment := range input.SlotAssignments {
		if _, err := t.deviceSets.SetRackSlotPosition(ctx, connect.NewRequest(&devicesetv1.SetRackSlotPositionRequest{
			DeviceSetId:      input.RackID,
			DeviceIdentifier: assignment.DeviceIdentifier,
			Position:         &devicesetv1.RackSlotPosition{Row: assignment.Row, Column: assignment.Column},
		})); err != nil {
			return chatdomain.ToolOutput{}, err
		}
	}
	content, err := json.Marshal(map[string]any{
		"applied":          true,
		"rack_id":          input.RackID,
		"rack_label":       rack.GetLabel(),
		"assigned_count":   len(input.SlotAssignments),
		"slot_assignments": assignmentViews,
	})
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal assigned rack slots: %w", err)
	}
	return chatdomain.ToolOutput{
		Content: string(content),
		Summary: fmt.Sprintf("Assigned %d slot(s) in rack %d", len(input.SlotAssignments), input.RackID),
	}, nil
}

func (t *FleetTools) clearRackSlots(ctx context.Context, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	input, err := buildClearRackSlotsInput(arguments)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	rack, members, existingSlots, err := t.loadRackSlotState(ctx, input.RackID)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	memberSet := makeStringSet(members)
	for _, identifier := range input.DeviceIdentifiers {
		if _, ok := memberSet[identifier]; !ok {
			return chatdomain.ToolOutput{}, fmt.Errorf("invalid clear_rack_slots arguments: miner %q is not assigned to rack %d", identifier, input.RackID)
		}
	}
	clearDevices := makeStringSet(input.DeviceIdentifiers)
	cleared := 0
	for _, slot := range existingSlots {
		if _, shouldClear := clearDevices[slot.GetDeviceIdentifier()]; shouldClear {
			cleared++
		}
	}
	for _, identifier := range input.DeviceIdentifiers {
		if _, err := t.deviceSets.ClearRackSlotPosition(ctx, connect.NewRequest(&devicesetv1.ClearRackSlotPositionRequest{
			DeviceSetId:      input.RackID,
			DeviceIdentifier: identifier,
		})); err != nil {
			return chatdomain.ToolOutput{}, err
		}
	}
	content, err := json.Marshal(map[string]any{
		"cleared":            true,
		"rack_id":            input.RackID,
		"rack_label":         rack.GetLabel(),
		"requested_count":    len(input.DeviceIdentifiers),
		"cleared_count":      cleared,
		"device_identifiers": input.DeviceIdentifiers,
	})
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal cleared rack slots: %w", err)
	}
	return chatdomain.ToolOutput{
		Content: string(content),
		Summary: fmt.Sprintf("Cleared %d slot(s) in rack %d", cleared, input.RackID),
	}, nil
}

func (t *FleetTools) loadRackSlotState(ctx context.Context, rackID int64) (*devicesetv1.DeviceSet, []string, []*devicesetv1.RackSlot, error) {
	rackResponse, err := t.deviceSets.GetDeviceSet(ctx, connect.NewRequest(&devicesetv1.GetDeviceSetRequest{DeviceSetId: rackID}))
	if err != nil {
		return nil, nil, nil, err
	}
	rack := rackResponse.Msg.GetDeviceSet()
	if rack.GetType() != devicesetv1.DeviceSetType_DEVICE_SET_TYPE_RACK {
		return nil, nil, nil, fmt.Errorf("invalid rack slot arguments: device set %d is not a rack", rackID)
	}

	members, err := t.listRackMemberIdentifiers(ctx, rackID)
	if err != nil {
		return nil, nil, nil, err
	}
	slotsResponse, err := t.deviceSets.GetRackSlots(ctx, connect.NewRequest(&devicesetv1.GetRackSlotsRequest{DeviceSetId: rackID}))
	if err != nil {
		return nil, nil, nil, err
	}
	return rack, members, slotsResponse.Msg.GetSlots(), nil
}

func (t *FleetTools) listRackMemberIdentifiers(ctx context.Context, rackID int64) ([]string, error) {
	const pageSize = 1000
	members := make([]string, 0)
	pageToken := ""
	for {
		response, err := t.deviceSets.ListDeviceSetMembers(ctx, connect.NewRequest(&devicesetv1.ListDeviceSetMembersRequest{
			DeviceSetId: rackID,
			PageSize:    pageSize,
			PageToken:   pageToken,
		}))
		if err != nil {
			return nil, err
		}
		for _, member := range response.Msg.GetMembers() {
			members = append(members, member.GetDeviceIdentifier())
		}
		pageToken = response.Msg.GetNextPageToken()
		if pageToken == "" {
			break
		}
	}
	return members, nil
}

func validateSetRackSlots(input setRackSlotsInput, rack *devicesetv1.DeviceSet, members []string, existingSlots []*devicesetv1.RackSlot) error {
	rackInfo := rack.GetRackInfo()
	if rackInfo.GetRows() <= 0 || rackInfo.GetColumns() <= 0 {
		return fmt.Errorf("invalid set_rack_slots arguments: rack %d does not have a usable slot layout", input.RackID)
	}
	memberSet := makeStringSet(members)
	requestedDevices := make(map[string]struct{}, len(input.SlotAssignments))
	requestedPositions := make(map[string]rackSlotAssignmentInput, len(input.SlotAssignments))
	for _, assignment := range input.SlotAssignments {
		if _, ok := memberSet[assignment.DeviceIdentifier]; !ok {
			return fmt.Errorf("invalid set_rack_slots arguments: miner %q is not assigned to rack %d", assignment.DeviceIdentifier, input.RackID)
		}
		if assignment.Row >= rackInfo.GetRows() {
			return fmt.Errorf("invalid set_rack_slots arguments: row %d is out of bounds for rack %d (%d rows, 0-indexed)", assignment.Row, input.RackID, rackInfo.GetRows())
		}
		if assignment.Column >= rackInfo.GetColumns() {
			return fmt.Errorf("invalid set_rack_slots arguments: column %d is out of bounds for rack %d (%d columns, 0-indexed)", assignment.Column, input.RackID, rackInfo.GetColumns())
		}
		requestedDevices[assignment.DeviceIdentifier] = struct{}{}
		requestedPositions[rackSlotPositionKey(assignment.Row, assignment.Column)] = assignment
	}
	for _, slot := range existingSlots {
		assignment, positionRequested := requestedPositions[rackSlotPositionKey(slot.GetPosition().GetRow(), slot.GetPosition().GetColumn())]
		if !positionRequested {
			continue
		}
		if slot.GetDeviceIdentifier() == assignment.DeviceIdentifier {
			continue
		}
		if _, movingOccupant := requestedDevices[slot.GetDeviceIdentifier()]; movingOccupant {
			continue
		}
		return fmt.Errorf(
			"invalid set_rack_slots arguments: slot (%d,%d) is already occupied by miner %q; include that miner in the request or choose an empty slot",
			assignment.Row,
			assignment.Column,
			slot.GetDeviceIdentifier(),
		)
	}
	return nil
}

func makeStringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func rackSlotPositionKey(row, column int32) string {
	return fmt.Sprintf("%d:%d", row, column)
}

type unsupportedMinerGroupView struct {
	FirmwareVersion string `json:"firmware_version,omitempty"`
	Model           string `json:"model,omitempty"`
	Count           int32  `json:"count"`
}

func (t *FleetTools) previewMinerAction(ctx context.Context, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	if t.commands == nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("preview_miner_action is unavailable")
	}
	input, commandType, selector, selection, err := buildMinerActionInput(arguments, "preview_miner_action")
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	response, err := t.commands.CheckCommandCapabilities(ctx, connect.NewRequest(&minercommandv1.CheckCommandCapabilitiesRequest{
		CommandType:    commandType,
		DeviceSelector: selector,
	}))
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	unsupportedGroups := make([]unsupportedMinerGroupView, 0, len(response.Msg.GetUnsupportedGroups()))
	for _, group := range response.Msg.GetUnsupportedGroups() {
		unsupportedGroups = append(unsupportedGroups, unsupportedMinerGroupView{
			FirmwareVersion: group.GetFirmwareVersion(),
			Model:           group.GetModel(),
			Count:           group.GetCount(),
		})
	}
	payload := map[string]any{
		"action":                       normalizeToolEnum(input.Action),
		"selector":                     selection,
		"total_count":                  response.Msg.GetTotalCount(),
		"supported_count":              response.Msg.GetSupportedCount(),
		"unsupported_count":            response.Msg.GetUnsupportedCount(),
		"all_supported":                response.Msg.GetAllSupported(),
		"none_supported":               response.Msg.GetNoneSupported(),
		"supported_device_identifiers": response.Msg.GetSupportedDeviceIdentifiers(),
		"unsupported_groups":           unsupportedGroups,
	}
	if selection.Type == minerActionSelectorExplicit {
		payload["requested_device_identifiers"] = selection.DeviceIdentifiers
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal miner action preview: %w", err)
	}
	return chatdomain.ToolOutput{
		Content: string(content),
		Summary: fmt.Sprintf("Previewed %s for %s", strings.ReplaceAll(normalizeToolEnum(input.Action), "_", " "), minerActionTargetPhrase(selection)),
	}, nil
}

func (t *FleetTools) executeMinerAction(ctx context.Context, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	if t.commands == nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("execute_miner_action is unavailable")
	}
	input, commandType, selector, selection, err := buildMinerActionInput(arguments, "execute_miner_action")
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	var batchIdentifier string
	switch commandType {
	case minercommandv1.CommandType_COMMAND_TYPE_REBOOT:
		response, err := t.commands.Reboot(ctx, connect.NewRequest(&minercommandv1.RebootRequest{DeviceSelector: selector}))
		if err != nil {
			return chatdomain.ToolOutput{}, err
		}
		batchIdentifier = response.Msg.GetBatchIdentifier()
	case minercommandv1.CommandType_COMMAND_TYPE_START_MINING:
		response, err := t.commands.StartMining(ctx, connect.NewRequest(&minercommandv1.StartMiningRequest{DeviceSelector: selector}))
		if err != nil {
			return chatdomain.ToolOutput{}, err
		}
		batchIdentifier = response.Msg.GetBatchIdentifier()
	case minercommandv1.CommandType_COMMAND_TYPE_STOP_MINING:
		response, err := t.commands.StopMining(ctx, connect.NewRequest(&minercommandv1.StopMiningRequest{DeviceSelector: selector}))
		if err != nil {
			return chatdomain.ToolOutput{}, err
		}
		batchIdentifier = response.Msg.GetBatchIdentifier()
	case minercommandv1.CommandType_COMMAND_TYPE_BLINK_LED:
		response, err := t.commands.BlinkLED(ctx, connect.NewRequest(&minercommandv1.BlinkLEDRequest{DeviceSelector: selector}))
		if err != nil {
			return chatdomain.ToolOutput{}, err
		}
		batchIdentifier = response.Msg.GetBatchIdentifier()
	case minercommandv1.CommandType_COMMAND_TYPE_UNSPECIFIED,
		minercommandv1.CommandType_COMMAND_TYPE_SET_COOLING_MODE,
		minercommandv1.CommandType_COMMAND_TYPE_UPDATE_MINING_POOLS,
		minercommandv1.CommandType_COMMAND_TYPE_DOWNLOAD_LOGS,
		minercommandv1.CommandType_COMMAND_TYPE_FIRMWARE_UPDATE,
		minercommandv1.CommandType_COMMAND_TYPE_SET_POWER_TARGET,
		minercommandv1.CommandType_COMMAND_TYPE_UPDATE_MINER_PASSWORD,
		minercommandv1.CommandType_COMMAND_TYPE_CURTAIL,
		minercommandv1.CommandType_COMMAND_TYPE_UNCURTAIL:
		return chatdomain.ToolOutput{}, fmt.Errorf("invalid execute_miner_action arguments: unsupported action %q", input.Action)
	default:
		return chatdomain.ToolOutput{}, fmt.Errorf("invalid execute_miner_action arguments: unsupported action %q", input.Action)
	}
	action := normalizeToolEnum(input.Action)
	payload := map[string]any{
		"executed":                   true,
		"action":                     action,
		"selector":                   selection,
		"command_batch_identifier":   batchIdentifier,
		"command_status_lookup_hint": "Use the existing command activity UI to inspect per-miner command results.",
	}
	if selection.Type == minerActionSelectorExplicit {
		payload["selected_count"] = len(selection.DeviceIdentifiers)
		payload["device_identifiers"] = selection.DeviceIdentifiers
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal executed miner action: %w", err)
	}
	return chatdomain.ToolOutput{
		Content: string(content),
		Summary: fmt.Sprintf("Submitted %s for %s", strings.ReplaceAll(action, "_", " "), minerActionTargetPhrase(selection)),
	}, nil
}

func (t *FleetTools) previewDowntimeWindow(_ context.Context, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	input, request, err := buildDowntimeWindowRequest(arguments)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	payload := downtimeWindowPayload(input, request, 0)
	payload["will_create"] = false
	content, err := json.Marshal(payload)
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal downtime window preview: %w", err)
	}
	return chatdomain.ToolOutput{
		Content: string(content),
		Summary: fmt.Sprintf("Previewed downtime schedule %q", input.Name),
	}, nil
}

func (t *FleetTools) createDowntimeWindow(ctx context.Context, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	if t.schedules == nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("create_downtime_window is unavailable")
	}
	input, request, err := buildDowntimeWindowRequest(arguments)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	response, err := t.schedules.CreateSchedule(ctx, connect.NewRequest(request))
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	payload := downtimeWindowPayload(input, request, response.Msg.GetSchedule().GetId())
	payload["created"] = true
	content, err := json.Marshal(payload)
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal created downtime window: %w", err)
	}
	return chatdomain.ToolOutput{
		Content: string(content),
		Summary: fmt.Sprintf("Created schedule %q", input.Name),
	}, nil
}

func downtimeWindowPayload(input downtimeWindowInput, request *schedulev1.CreateScheduleRequest, scheduleID int64) map[string]any {
	payload := map[string]any{
		"schedule_id":   scheduleID,
		"name":          request.GetName(),
		"action":        scheduleActionLabel(request.GetAction()),
		"schedule_type": "one_time",
		"start_date":    request.GetStartDate(),
		"start_time":    request.GetStartTime(),
		"end_date":      request.GetEndDate(),
		"end_time":      request.GetEndTime(),
		"timezone":      request.GetTimezone(),
		"targets":       input.Targets,
		"target_count":  len(request.GetTargets()),
		"operator_note": downtimeActionNote(request),
	}
	if request.GetAction() == schedulev1.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET {
		payload["power_target_mode"] = powerTargetModeLabel(request.GetActionConfig().GetMode())
	}
	return payload
}

func downtimeActionNote(request *schedulev1.CreateScheduleRequest) string {
	switch request.GetAction() {
	case schedulev1.ScheduleAction_SCHEDULE_ACTION_SLEEP:
		return "Sleep schedules stop mining at start_time and do not automatically resume at end_time."
	case schedulev1.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET:
		if request.GetEndTime() != "" {
			return "Set-power-target schedules with end_time revert through the existing scheduler behavior."
		}
		return "Set-power-target schedules without end_time do not define a bounded revert window."
	case schedulev1.ScheduleAction_SCHEDULE_ACTION_REBOOT:
		return "Reboot schedules run once at start_time."
	case schedulev1.ScheduleAction_SCHEDULE_ACTION_UNSPECIFIED:
		return "Schedule behavior follows the existing schedule service."
	default:
		return "Schedule behavior follows the existing schedule service."
	}
}

func scheduleActionLabel(action schedulev1.ScheduleAction) string {
	switch action {
	case schedulev1.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET:
		return "set_power_target"
	case schedulev1.ScheduleAction_SCHEDULE_ACTION_REBOOT:
		return "reboot"
	case schedulev1.ScheduleAction_SCHEDULE_ACTION_SLEEP:
		return "sleep"
	case schedulev1.ScheduleAction_SCHEDULE_ACTION_UNSPECIFIED:
		return labelUnspecified
	default:
		return labelUnspecified
	}
}

func powerTargetModeLabel(mode schedulev1.PowerTargetMode) string {
	switch mode {
	case schedulev1.PowerTargetMode_POWER_TARGET_MODE_DEFAULT:
		return "default"
	case schedulev1.PowerTargetMode_POWER_TARGET_MODE_MAX:
		return "max"
	case schedulev1.PowerTargetMode_POWER_TARGET_MODE_UNSPECIFIED:
		return labelUnspecified
	default:
		return labelUnspecified
	}
}

func formatDowntimeTargets(targets []downtimeTargetInput, limit int) string {
	parts := make([]string, 0, min(len(targets), limit)+1)
	for i, target := range targets {
		if i >= limit {
			parts = append(parts, fmt.Sprintf("+%d more", len(targets)-limit))
			break
		}
		parts = append(parts, fmt.Sprintf("%s:%s", normalizeToolEnum(target.Type), target.TargetID))
	}
	return strings.Join(parts, ", ")
}

func (t *FleetTools) createSite(ctx context.Context, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	input, request, err := buildCreateSiteRequest(arguments)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	response, err := t.sites.CreateSite(ctx, connect.NewRequest(request))
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	warnings := response.Msg.GetNetworkConfigWarnings()
	if warnings == nil {
		warnings = []string{}
	}
	payload := map[string]any{
		"created":  true,
		"site_id":  response.Msg.GetSite().GetId(),
		"name":     response.Msg.GetSite().GetName(),
		"warnings": warnings,
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal created site: %w", err)
	}
	return chatdomain.ToolOutput{
		Content: string(content),
		Summary: fmt.Sprintf("Created site %q", input.Name),
	}, nil
}

func (t *FleetTools) createRack(ctx context.Context, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	input, request, err := buildCreateRackRequest(arguments)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	response, err := t.deviceSets.CreateDeviceSet(ctx, connect.NewRequest(request))
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	payload := map[string]any{
		"created": true,
		"rack_id": response.Msg.GetDeviceSet().GetId(),
		"label":   response.Msg.GetDeviceSet().GetLabel(),
		"rows":    input.Rows,
		"columns": input.Columns,
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal created rack: %w", err)
	}
	return chatdomain.ToolOutput{
		Content: string(content),
		Summary: fmt.Sprintf("Created rack %q", input.Label),
	}, nil
}

func (t *FleetTools) moveMinersToRack(ctx context.Context, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	input, request, err := buildMoveMinersRequest(arguments)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	response, err := t.deviceSets.AssignDevicesToRack(ctx, connect.NewRequest(request))
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	if len(response.Msg.GetConflicts()) > 0 {
		conflictingMiners := make([]string, 0, len(response.Msg.GetConflicts()))
		for _, conflict := range response.Msg.GetConflicts() {
			conflictingMiners = append(conflictingMiners, conflict.GetDeviceIdentifier())
		}
		content, marshalErr := json.Marshal(map[string]any{
			"moved":              false,
			"requires_force":     true,
			"conflicting_miners": conflictingMiners,
			"reason":             "destination rack is unassigned; moving these miners would clear their site and building",
		})
		if marshalErr != nil {
			return chatdomain.ToolOutput{}, fmt.Errorf("marshal rack conflicts: %w", marshalErr)
		}
		return chatdomain.ToolOutput{
			Content: string(content),
			Summary: fmt.Sprintf("No miners moved; %d placement conflict(s) need confirmation", len(conflictingMiners)),
		}, nil
	}
	payload := map[string]any{
		"moved":                 true,
		"target_rack_id":        input.TargetRackID,
		"assigned_count":        response.Msg.GetAssignedCount(),
		"removed_count":         response.Msg.GetRemovedCount(),
		"site_reassigned_count": response.Msg.GetSiteReassignedCount(),
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return chatdomain.ToolOutput{}, fmt.Errorf("marshal moved miners: %w", err)
	}
	return chatdomain.ToolOutput{
		Content: string(content),
		Summary: fmt.Sprintf("Moved %d miner(s) to rack %d", response.Msg.GetAssignedCount(), input.TargetRackID),
	}, nil
}
