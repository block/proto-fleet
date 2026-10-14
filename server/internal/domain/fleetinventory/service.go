// Package fleetinventory provides a shared read model for distinct physical entities.
package fleetinventory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/containerreadings"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	fleetpb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type MinerReader interface {
	InventorySnapshots(ctx context.Context, orgID int64, identifiers []string, canReadSite func(*int64) bool) ([]*fleetpb.MinerStateSnapshot, error)
}

type Store interface {
	GetHistoryResolution(ctx context.Context, params sqlc.GetHistoryResolutionParams) (int32, error)
	TankStore
	HashboardStore
	ListFleetInventoryOptions(ctx context.Context, params sqlc.ListFleetInventoryOptionsParams) (sqlc.ListFleetInventoryOptionsRow, error)
	ListContainers(ctx context.Context, params sqlc.ListContainersParams) ([]sqlc.ListContainersRow, error)
	ListFleetFacilities(ctx context.Context, params sqlc.ListFleetFacilitiesParams) (sqlc.ListFleetFacilitiesRow, error)
	ListFleetEquipmentGroups(ctx context.Context, params sqlc.ListFleetEquipmentGroupsParams) (sqlc.ListFleetEquipmentGroupsRow, error)
	ListFleetMiningAssets(ctx context.Context, params sqlc.ListFleetMiningAssetsParams) (sqlc.ListFleetMiningAssetsRow, error)
	GetContainerModuleScope(ctx context.Context, params sqlc.GetContainerModuleScopeParams) (sqlc.GetContainerModuleScopeRow, error)
	ListContainerModuleHistory(ctx context.Context, params sqlc.ListContainerModuleHistoryParams) ([]sqlc.ListContainerModuleHistoryRow, error)
}

type TankStore interface {
	ListFleetTankOptions(ctx context.Context, params sqlc.ListFleetTankOptionsParams) ([]sqlc.ListFleetTankOptionsRow, error)
	GetContainerTankScope(ctx context.Context, params sqlc.GetContainerTankScopeParams) (sql.NullInt64, error)
	GetContainerTankHistory(ctx context.Context, params sqlc.GetContainerTankHistoryParams) ([]sqlc.GetContainerTankHistoryRow, error)
}

type HashboardStore interface {
	CanReadHashboardHistory(ctx context.Context, params sqlc.CanReadHashboardHistoryParams) (bool, error)
	GetModuleHashboards(ctx context.Context, params sqlc.GetModuleHashboardsParams) (json.RawMessage, error)
	ListHashboardHistory(ctx context.Context, params sqlc.ListHashboardHistoryParams) ([]sqlc.ListHashboardHistoryRow, error)
}

type Service struct {
	store     Store
	buildings interfaces.BuildingStore
	miners    MinerReader
}

func NewService(store Store) *Service { return &Service{store: store} }

func (s *Service) WithBuildings(buildings interfaces.BuildingStore) *Service {
	s.buildings = buildings
	return s
}

func (s *Service) WithMiners(miners MinerReader) *Service { s.miners = miners; return s }

// Scope represents one required permission. Telemetry-only scopes restrict
// building telemetry without restricting its independently readable metadata.
type Scope struct {
	// RackOnly applies the extra equipment-group permission to rack rows, not tanks.
	RackOnly bool `json:"rack_only,omitempty"`
	// TelemetryOnly preserves building metadata access independently of telemetry.
	TelemetryOnly bool    `json:"telemetry_only,omitempty"`
	OrgWide       bool    `json:"org_wide"`
	Sites         []int64 `json:"sites"`
}

type pageCursor struct {
	ResolutionSeconds *int32    `json:"resolution_seconds,omitempty"`
	Fingerprint       string    `json:"query"`
	Kind              int32     `json:"kind"`
	ID                int64     `json:"id"`
	Number            *float64  `json:"number"`
	Text              string    `json:"text"`
	Time              time.Time `json:"time"`
	CollectedAt       time.Time `json:"collected_at"`
}

func fingerprint(orgID int64, request proto.Message, scopes []Scope) (string, error) {
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("encode inventory query fingerprint: %w", err)
	}
	s, err := json.Marshal(scopes)
	if err != nil {
		return "", fmt.Errorf("encode inventory query fingerprint: %w", err)
	}
	h := sha256.Sum256(append(append([]byte(fmt.Sprintf("%d\x00%s\x00", orgID, request.ProtoReflect().Descriptor().FullName())), b...), s...))
	return hex.EncodeToString(h[:]), nil
}

func decodeCursor(token, fingerprint string) (pageCursor, error) {
	c := pageCursor{Fingerprint: fingerprint}
	if token == "" {
		return c, nil
	}
	if len(token) > 4096 {
		return c, fleeterror.NewInvalidArgumentError("invalid page token")
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || json.Unmarshal(b, &c) != nil || c.Fingerprint != fingerprint || (c.Number != nil && (math.IsNaN(*c.Number) || math.IsInf(*c.Number, 0))) {
		return c, fleeterror.NewInvalidArgumentError("page token does not match this query")
	}
	return c, nil
}

func encodeCursor(c pageCursor) (string, error) {
	b, err := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b), err
}

type listRow struct {
	Coverage                  *sdk.ContainerRollup       `json:"coverage"`
	ModuleIssues              *sdk.ContainerModuleIssues `json:"module_issues"`
	ModuleIssuesLastSeenAt    *time.Time                 `json:"module_issues_last_seen_at"`
	TemperatureReportingCount int64                      `json:"temperature_reporting_count"`
	EfficiencyReportingCount  int64                      `json:"efficiency_reporting_count"`
	TelemetryAllowed          bool                       `json:"telemetry_allowed"`
	BuildingDetails           json.RawMessage            `json:"building_details"`
	RackDetails               json.RawMessage            `json:"rack_details"`
	SiteName                  string                     `json:"site_name"`
	FacilityName              string                     `json:"facility_name"`
	GroupName                 string                     `json:"group_name"`
	RackCount                 int64                      `json:"rack_count"`
	HashingCount              int32                      `json:"hashing_count"`
	SleepingCount             int32                      `json:"sleeping_count"`
	OfflineCount              int32                      `json:"offline_count"`
	BrokenCount               int32                      `json:"broken_count"`
	PSUIssues                 int32                      `json:"psu_issues"`
	FanIssues                 int32                      `json:"fan_issues"`
	HashboardIssues           int32                      `json:"hashboard_issues"`
	ControlIssues             int32                      `json:"control_issues"`

	Kind                   int32      `json:"kind"`
	ID                     int64      `json:"id"`
	SiteID                 *int64     `json:"site_id"`
	Name                   string     `json:"name"`
	DeviceIdentifier       string     `json:"device_identifier"`
	Position               int32      `json:"position"`
	ReportedID             string     `json:"reported_id"`
	FacilityKind           int32      `json:"facility_kind"`
	FacilityID             *int64     `json:"facility_id"`
	GroupKind              int32      `json:"group_kind"`
	GroupID                *int64     `json:"group_id"`
	State                  string     `json:"state"`
	AssetCount             int64      `json:"asset_count"`
	HashrateReportingCount int64      `json:"hashrate_reporting_count"`
	PowerReportingCount    int64      `json:"power_reporting_count"`
	HashrateTHS            *float64   `json:"hashrate_ths"`
	PowerKW                *float64   `json:"power_kw"`
	EfficiencyJTH          *float64   `json:"efficiency_jth"`
	MinTemperatureC        *float64   `json:"min_temperature_c"`
	MaxTemperatureC        *float64   `json:"max_temperature_c"`
	ObservedAt             *time.Time `json:"observed_at"`
	ReportedHashrateTHS    *float64   `json:"reported_hashrate_ths"`
	ReportedPowerKW        *float64   `json:"reported_power_kw"`
	SortNumber             *float64   `json:"sort_number"`
	SortText               string     `json:"sort_text"`
}

func (r listRow) item() *pb.InventoryItem {
	item := &pb.InventoryItem{Coverage: containerreadings.Coverage(r.Coverage), Ref: &pb.EntityRef{Kind: pb.EntityKind(r.Kind), Id: r.ID}, Name: r.Name, SiteId: r.SiteID,
		DeviceIdentifier: r.DeviceIdentifier, Position: r.Position, ReportedId: r.ReportedID, State: r.State,
		AssetCount: r.AssetCount, HashrateReportingCount: r.HashrateReportingCount, PowerReportingCount: r.PowerReportingCount,
		HashrateThs: r.HashrateTHS, PowerKw: r.PowerKW, EfficiencyJth: r.EfficiencyJTH,
		MinTemperatureC: r.MinTemperatureC, MaxTemperatureC: r.MaxTemperatureC,
		ReportedHashrateThs: r.ReportedHashrateTHS, ReportedPowerKw: r.ReportedPowerKW}
	if r.FacilityID != nil {
		item.Ancestors = append(item.Ancestors, &pb.EntityRef{Kind: pb.EntityKind(r.FacilityKind), Id: *r.FacilityID})
	}
	if r.GroupID != nil {
		item.Ancestors = append(item.Ancestors, &pb.EntityRef{Kind: pb.EntityKind(r.GroupKind), Id: *r.GroupID})
	}
	if r.ObservedAt != nil {
		item.ObservedAt = timestamppb.New(*r.ObservedAt)
	}
	item.ModuleIssues = projectModuleIssues(r.ModuleIssues, r.ModuleIssuesLastSeenAt, time.Now())
	return item
}

func (s *Service) List(ctx context.Context, orgID int64, req *pb.ListInventoryRequest, scopes []Scope) (*pb.ListInventoryResponse, error) {
	params, err := listParams(orgID, req, scopes)
	if err != nil {
		return nil, err
	}
	params.SpecialFilters, err = s.specialFilters(ctx, orgID, req)
	if err != nil {
		return nil, err
	}
	keyRequest, ok := proto.Clone(req).(*pb.ListInventoryRequest)
	if !ok {
		return nil, fleeterror.NewInternalError("unexpected inventory request type")
	}
	keyRequest.PageToken, keyRequest.PageSize = "", 0
	key, err := fingerprint(orgID, keyRequest, scopes)
	if err != nil {
		return nil, err
	}
	cursor, err := decodeCursor(req.PageToken, key)
	if err != nil {
		return nil, err
	}
	if req.PageToken != "" && (cursor.ID <= 0 || cursor.Kind < 1 || cursor.Kind > 6) {
		return nil, fleeterror.NewInvalidArgumentError("invalid inventory cursor")
	}
	params.HasCursor, params.CursorNull = req.PageToken != "", cursor.Number == nil
	params.CursorText, params.CursorKind, params.CursorID = cursor.Text, cursor.Kind, cursor.ID
	if cursor.Number != nil {
		params.CursorNumber = *cursor.Number
	}
	result, err := s.list(ctx, req.View, params)
	if err != nil {
		return nil, err
	}
	var rows []listRow
	if err := json.Unmarshal(result.Items, &rows); err != nil {
		return nil, fmt.Errorf("decode inventory rows: %w", err)
	}
	out := &pb.ListInventoryResponse{TotalCount: result.TotalCount, MinerCount: result.MinerCount}
	if req.View == pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS {
		options, optionsErr := s.store.ListFleetInventoryOptions(ctx, sqlc.ListFleetInventoryOptionsParams{OrgID: orgID, Authority: params.Authority})
		if optionsErr != nil {
			return nil, optionsErr
		}
		out.Models, out.FirmwareVersions = options.Models, options.FirmwareVersions
	}

	if len(rows) == int(params.PageLimit) {
		rows = rows[:len(rows)-1]
		last := rows[len(rows)-1]
		out.NextPageToken, err = encodeCursor(pageCursor{Fingerprint: key, Kind: last.Kind, ID: last.ID, Number: last.SortNumber, Text: last.SortText})
		if err != nil {
			return nil, err
		}
	}
	for _, row := range rows {
		out.Items = append(out.Items, row.item())
	}
	if err := s.enrich(ctx, orgID, rows, out.Items, scopes); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) History(ctx context.Context, orgID int64, req *pb.ListModuleHistoryRequest, scopes []Scope) (*pb.ListModuleHistoryResponse, error) {
	if req == nil || req.ModuleId <= 0 || req.StartTime == nil || req.EndTime == nil || req.StartTime.CheckValid() != nil || req.EndTime.CheckValid() != nil || !req.StartTime.AsTime().Before(req.EndTime.AsTime()) || req.PageSize < 0 || req.PageSize > 1000 {
		return nil, fleeterror.NewInvalidArgumentError("invalid module history request")
	}
	resource, err := s.store.GetContainerModuleScope(ctx, sqlc.GetContainerModuleScopeParams{ModuleID: req.ModuleId, OrgID: orgID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fleeterror.NewNotFoundError("module not found")
	}
	if err != nil {
		return nil, err
	}
	if !allows(scopes, resource.SiteID) {
		return nil, fleeterror.NewForbiddenError("module history is not accessible")
	}
	keyRequest, ok := proto.Clone(req).(*pb.ListModuleHistoryRequest)
	if !ok {
		return nil, fleeterror.NewInternalError("unexpected inventory request type")
	}
	keyRequest.PageToken, keyRequest.PageSize = "", 0
	key, err := fingerprint(orgID, keyRequest, scopes)
	if err != nil {
		return nil, err
	}
	cursor, err := decodeCursor(req.PageToken, key)
	if err != nil {
		return nil, err
	}
	resolution, err := s.historyPageResolution(ctx, req.PageToken, cursor, req.StartTime.AsTime(), req.EndTime.AsTime())
	if err != nil {
		return nil, err
	}

	size := req.PageSize
	if size == 0 {
		size = 50
	}
	authority, err := marshalScopes(scopes)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ListContainerModuleHistory(ctx, sqlc.ListContainerModuleHistoryParams{ResolutionSeconds: resolution, ModuleID: req.ModuleId, OrgID: orgID, Authority: authority, StartTime: req.StartTime.AsTime(), EndTime: req.EndTime.AsTime(), PageLimit: size + 1, HasCursor: req.PageToken != "", AfterTime: cursor.Time, AfterCollectedAt: cursor.CollectedAt})
	if err != nil {
		return nil, err
	}
	out := &pb.ListModuleHistoryResponse{}
	if len(rows) > int(size) {
		rows = rows[:size]
		out.NextPageToken, err = encodeCursor(pageCursor{Fingerprint: key, Time: rows[len(rows)-1].Time, CollectedAt: rows[len(rows)-1].CollectedAt, ResolutionSeconds: &resolution})
		if err != nil {
			return nil, err
		}
	}
	for _, r := range rows {
		v := &pb.ModuleSample{ObservedAt: timestamppb.New(r.Time), CollectedAt: timestamppb.New(r.CollectedAt)}
		if r.HashrateHs.Valid {
			v.HashrateHs = &r.HashrateHs.Float64
		}
		if r.PowerW.Valid {
			v.PowerW = &r.PowerW.Float64
		}
		if r.EfficiencyJh.Valid {
			v.EfficiencyJh = &r.EfficiencyJh.Float64
		}
		if r.Coverage.Valid {
			var coverage sdk.ContainerRollup
			if err := json.Unmarshal(r.Coverage.RawMessage, &coverage); err != nil {
				return nil, fmt.Errorf("decode module coverage: %w", err)
			}
			v.Coverage = containerreadings.Coverage(&coverage)
		}
		if r.TemperatureC.Valid {
			v.TemperatureC = &r.TemperatureC.Float64
		}
		out.Samples = append(out.Samples, v)
	}
	return out, nil
}

func allows(scopes []Scope, site sql.NullInt64) bool {
	if len(scopes) == 0 {
		return false
	}
	for _, scope := range scopes {
		listed := false
		for _, id := range scope.Sites {
			if site.Valid && site.Int64 == id {
				listed = true
			}
		}
		if (scope.OrgWide && listed) || (!scope.OrgWide && !listed) {
			return false
		}
	}
	return true
}
