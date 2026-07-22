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
	poolsv1 "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	sitesv1 "github.com/block/proto-fleet/server/generated/grpc/sites/v1"
	chatdomain "github.com/block/proto-fleet/server/internal/domain/chat"
)

const (
	defaultResolveMinersLimit = 100
	maxResolveMinersLimit     = 1000
	maxResolveMinersScan      = 5000
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
	ListDeviceSets(ctx context.Context, req *connect.Request[devicesetv1.ListDeviceSetsRequest]) (*connect.Response[devicesetv1.ListDeviceSetsResponse], error)
	CreateDeviceSet(ctx context.Context, req *connect.Request[devicesetv1.CreateDeviceSetRequest]) (*connect.Response[devicesetv1.CreateDeviceSetResponse], error)
	AssignDevicesToRack(ctx context.Context, req *connect.Request[devicesetv1.AssignDevicesToRackRequest]) (*connect.Response[devicesetv1.AssignDevicesToRackResponse], error)
}

type FleetTools struct {
	fleet      fleetHandler
	sites      sitesHandler
	pools      poolsHandler
	deviceSets deviceSetsHandler
}

func NewFleetTools(fleet fleetHandler, sites sitesHandler, pools poolsHandler, deviceSets deviceSetsHandler) *FleetTools {
	return &FleetTools{fleet: fleet, sites: sites, pools: pools, deviceSets: deviceSets}
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
			Name:        "list_pools",
			Description: "List saved mining pool names. Connection URLs, usernames, wallet identifiers, worker identifiers, and credentials are never returned.",
			InputSchema: emptyObjectSchema(),
		},
		{
			Name:        "list_racks",
			Description: "List racks with IDs, labels, layouts, placement labels, and miner counts so an operator request can be resolved to an exact rack.",
			InputSchema: emptyObjectSchema(),
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
					"order_index":  map[string]any{"type": "string", "enum": []string{"top_left", "top_right", "bottom_left", "bottom_right"}},
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
	}
}

func emptyObjectSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}}
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
	for _, id := range ids {
		if id <= 0 {
			return fmt.Errorf("invalid resolve_miners arguments: %s must contain only positive IDs", field)
		}
	}
	return nil
}

func parseResolveMinerStatus(status string) (fleetv1.DeviceStatus, error) {
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
		return fleetv1.DeviceStatus_DEVICE_STATUS_UNSPECIFIED, fmt.Errorf("invalid resolve_miners arguments: unsupported device_status %q", status)
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
	case "", "top_left":
	case "top_right":
		orderIndex = devicesetv1.RackOrderIndex_RACK_ORDER_INDEX_TOP_RIGHT
	case "bottom_left":
		orderIndex = devicesetv1.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT
	case "bottom_right":
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
			orderIndex = "top_left"
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
	case "list_pools":
		return t.listPools(ctx)
	case "list_racks":
		return t.listRacks(ctx)
	case "create_site":
		return t.createSite(ctx, arguments)
	case "create_rack":
		return t.createRack(ctx, arguments)
	case "move_miners_to_rack":
		return t.moveMinersToRack(ctx, arguments)
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

func (t *FleetTools) resolveMiners(ctx context.Context, arguments json.RawMessage) (chatdomain.ToolOutput, error) {
	input, filter, err := buildResolveMinersFilter(arguments)
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}

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
	miners := make([]resolvedMinerView, 0, min(int(input.Limit), defaultResolveMinersLimit))

	for {
		response, err := t.fleet.ListMinerStateSnapshots(ctx, connect.NewRequest(&fleetv1.ListMinerStateSnapshotsRequest{
			PageSize: pageSize,
			Cursor:   cursor,
			Filter:   filter,
		}))
		if err != nil {
			return chatdomain.ToolOutput{}, err
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
			miners = append(miners, resolvedMinerViewFromSnapshot(miner))
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

	deviceIdentifiers := make([]string, 0, len(miners))
	for _, miner := range miners {
		deviceIdentifiers = append(deviceIdentifiers, miner.DeviceIdentifier)
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

func (t *FleetTools) listRacks(ctx context.Context) (chatdomain.ToolOutput, error) {
	response, err := t.deviceSets.ListDeviceSets(ctx, connect.NewRequest(&devicesetv1.ListDeviceSetsRequest{
		Type:     devicesetv1.DeviceSetType_DEVICE_SET_TYPE_RACK,
		PageSize: 1000,
	}))
	if err != nil {
		return chatdomain.ToolOutput{}, err
	}
	type rackView struct {
		ID          int64  `json:"id"`
		Label       string `json:"label"`
		Rows        int32  `json:"rows"`
		Columns     int32  `json:"columns"`
		DeviceCount int32  `json:"device_count"`
		Site        string `json:"site,omitempty"`
		Building    string `json:"building,omitempty"`
		Zone        string `json:"zone,omitempty"`
	}
	racks := make([]rackView, 0, len(response.Msg.GetDeviceSets()))
	for _, rack := range response.Msg.GetDeviceSets() {
		view := rackView{
			ID:          rack.GetId(),
			Label:       rack.GetLabel(),
			Rows:        rack.GetRackInfo().GetRows(),
			Columns:     rack.GetRackInfo().GetColumns(),
			DeviceCount: rack.GetDeviceCount(),
			Zone:        rack.GetRackInfo().GetZone(),
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
