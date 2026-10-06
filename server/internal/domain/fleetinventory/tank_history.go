package fleetinventory

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
	"unicode/utf8"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TankHistory returns chart buckets computed by the database, scoped to both
// the Fleet tank identity and its owning container. Current membership matches
// the tank overview; historical membership reconstruction is not implied.
func (s *Service) TankHistory(ctx context.Context, orgID int64, req *pb.GetTankHistoryRequest, scopes []Scope) (*pb.GetTankHistoryResponse, error) {
	if err := validateTankHistory(req); err != nil {
		return nil, err
	}
	site, err := s.store.GetContainerTankScope(ctx, sqlc.GetContainerTankScopeParams{TankID: req.TankId, ContainerIdentifier: req.ContainerIdentifier, OrgID: orgID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fleeterror.NewNotFoundError("tank not found")
	}
	if err != nil {
		return nil, err
	}
	if !allows(scopes, site) {
		return nil, fleeterror.NewForbiddenError("tank history is not accessible")
	}
	authority, err := marshalScopes(scopes)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.GetContainerTankHistory(ctx, sqlc.GetContainerTankHistoryParams{
		TankID: req.TankId, ContainerIdentifier: req.ContainerIdentifier, OrgID: orgID, Authority: authority,
		StartTime: req.StartTime.AsTime(), EndTime: req.EndTime.AsTime(), GranularitySeconds: req.GranularitySeconds,
	})
	if err != nil {
		return nil, err
	}
	response := &pb.GetTankHistoryResponse{}
	for _, row := range rows {
		metric := &telemetrypb.Metric{
			MeasurementType: telemetrypb.MeasurementType(row.MeasurementType),
			OpenTime:        timestamppb.New(time.Unix(row.Bucket, 0)), DeviceCount: row.ModuleCount,
		}
		// COALESCE in SQL supplies typed values for sqlc; only HasValues permits
		// exposing them. Missing and non-finite values are gaps, never zeroes.
		if row.HasValues && finite(row.Average) {
			metric.AggregatedValues = append(metric.AggregatedValues, &telemetrypb.AggregatedValue{AggregationType: telemetrypb.AggregationType_AGGREGATION_TYPE_AVERAGE, Value: row.Average})
			if row.HasMinimum && finite(row.Minimum) {
				metric.AggregatedValues = append(metric.AggregatedValues, &telemetrypb.AggregatedValue{AggregationType: telemetrypb.AggregationType_AGGREGATION_TYPE_MIN, Value: row.Minimum})
			}
			if row.HasMaximum && finite(row.Maximum) {
				metric.AggregatedValues = append(metric.AggregatedValues, &telemetrypb.AggregatedValue{AggregationType: telemetrypb.AggregationType_AGGREGATION_TYPE_MAX, Value: row.Maximum})
			}
		}
		response.Metrics = append(response.Metrics, metric)
	}
	return response, nil
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func validateTankHistory(req *pb.GetTankHistoryRequest) error {
	invalid := func() error {
		return fleeterror.NewInvalidArgumentError("tank history requires a valid time range of at most 366 days and at most 1000 buckets")
	}
	if req == nil || req.TankId <= 0 || req.ContainerIdentifier == "" || utf8.RuneCountInString(req.ContainerIdentifier) > 255 || req.StartTime == nil || req.EndTime == nil || req.StartTime.CheckValid() != nil || req.EndTime.CheckValid() != nil || req.GranularitySeconds < 1 || req.GranularitySeconds > 86400 {
		return invalid()
	}
	start, end := req.StartTime.AsTime(), req.EndTime.AsTime()
	if !start.Before(end) || end.Sub(start) > 366*24*time.Hour {
		return invalid()
	}
	// Include partially occupied boundary buckets, with an exclusive end time.
	width := float64(req.GranularitySeconds)
	first := math.Floor(float64(start.Unix()) / width)
	last := math.Floor(float64(end.Add(-time.Nanosecond).Unix()) / width)
	if last-first+1 > 1000 {
		return invalid()
	}
	return nil
}
