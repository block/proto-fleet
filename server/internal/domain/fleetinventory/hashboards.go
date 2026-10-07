package fleetinventory

import (
	"context"
	"encoding/json"
	"fmt"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Service) Hashboards(ctx context.Context, orgID int64, req *pb.GetModuleHashboardsRequest, scopes []Scope) (*pb.GetModuleHashboardsResponse, error) {
	if len(scopes) == 0 {
		return nil, fleeterror.NewForbiddenError("hashboard access requires read permissions")
	}
	if req == nil || len(req.ModuleIds) == 0 || len(req.ModuleIds) > 100 {
		return nil, fleeterror.NewInvalidArgumentError("request 1–100 modules")
	}
	seen := map[int64]bool{}
	for _, id := range req.ModuleIds {
		if id <= 0 || seen[id] {
			return nil, fleeterror.NewInvalidArgumentError("invalid or duplicate module")
		}
		seen[id] = true
	}
	authority, err := marshalScopes(scopes)
	if err != nil {
		return nil, err
	}
	data, err := s.store.GetModuleHashboards(ctx, sqlc.GetModuleHashboardsParams{ModuleIds: req.ModuleIds, OrgID: orgID, Authority: authority})
	if err != nil {
		return nil, err
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("decode hashboard modules: %w", err)
	}
	out := &pb.GetModuleHashboardsResponse{}
	for _, row := range rows {
		module := &pb.ModuleHashboards{}
		if err := protojson.Unmarshal(row, module); err != nil {
			return nil, fmt.Errorf("decode hashboard slots: %w", err)
		}
		out.Modules = append(out.Modules, module)
	}
	return out, nil
}

func (s *Service) HashboardHistory(ctx context.Context, orgID int64, req *pb.ListHashboardHistoryRequest, scopes []Scope) (*pb.ListHashboardHistoryResponse, error) {
	if len(scopes) == 0 {
		return nil, fleeterror.NewForbiddenError("hashboard access requires read permissions")
	}
	if req == nil || req.HashboardId <= 0 || req.StartTime == nil || req.EndTime == nil || req.StartTime.CheckValid() != nil || req.EndTime.CheckValid() != nil || !req.StartTime.AsTime().Before(req.EndTime.AsTime()) || req.PageSize < 0 || req.PageSize > 1000 {
		return nil, fleeterror.NewInvalidArgumentError("invalid hashboard history request")
	}
	authority, err := marshalScopes(scopes)
	if err != nil {
		return nil, err
	}
	allowed, err := s.store.CanReadHashboardHistory(ctx, sqlc.CanReadHashboardHistoryParams{HashboardID: req.HashboardId, OrgID: orgID, Authority: authority})
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, fleeterror.NewNotFoundError("hashboard not found or not accessible")
	}
	keyRequest, ok := proto.Clone(req).(*pb.ListHashboardHistoryRequest)
	if !ok {
		return nil, fleeterror.NewInternalError("unexpected history request")
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
	if req.PageToken != "" && (cursor.Time.Before(req.StartTime.AsTime()) || !cursor.Time.Before(req.EndTime.AsTime())) {
		return nil, fleeterror.NewInvalidArgumentError("invalid history cursor")
	}
	size := req.PageSize
	if size == 0 {
		size = 50
	}
	rows, err := s.store.ListHashboardHistory(ctx, sqlc.ListHashboardHistoryParams{HashboardID: req.HashboardId, OrgID: orgID, Authority: authority, StartTime: req.StartTime.AsTime(), EndTime: req.EndTime.AsTime(), PageLimit: size + 1, HasCursor: req.PageToken != "", AfterTime: cursor.Time, AfterCollectedAt: cursor.CollectedAt})
	if err != nil {
		return nil, err
	}
	out := &pb.ListHashboardHistoryResponse{}
	if len(rows) > int(size) {
		rows = rows[:size]
		last := rows[len(rows)-1]
		out.NextPageToken, err = encodeCursor(pageCursor{Fingerprint: key, Time: last.Time, CollectedAt: last.CollectedAt})
		if err != nil {
			return nil, err
		}
	}
	for _, r := range rows {
		v := &pb.HashboardReading{HashboardId: r.HashboardID, InstallationId: r.InstallationID, ModuleId: r.ModuleID, Position: r.Position, State: r.State, MiningState: r.MiningState, ObservedAt: timestamppb.New(r.Time), CollectedAt: timestamppb.New(r.CollectedAt)}
		if r.StateInInstallation && r.StateObservedAt.Valid {
			v.StateObservedAt = timestamppb.New(r.StateObservedAt.Time)
		}
		if r.Isolated.Valid {
			v.Isolated = &r.Isolated.Bool
		}
		if r.HashrateHs.Valid {
			v.HashrateHs = &r.HashrateHs.Float64
		}
		if r.TemperatureC.Valid {
			v.TemperatureC = &r.TemperatureC.Float64
		}
		if r.HashrailOk.Valid {
			v.HashrailOk = &r.HashrailOk.Bool
		}
		if r.RefclksOk.Valid {
			v.RefclksOk = &r.RefclksOk.Bool
		}
		if r.StateInInstallation && r.Health.Valid && string(r.Health.RawMessage) != "null" {
			v.Health = &structpb.Struct{}
			if err := protojson.Unmarshal(r.Health.RawMessage, v.Health); err != nil {
				return nil, fmt.Errorf("decode hashboard health: %w", err)
			}
		}
		out.Samples = append(out.Samples, v)
	}
	return out, nil
}
