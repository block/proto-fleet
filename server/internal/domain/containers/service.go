// Package containers projects controller observations into Fleet container cards.
package containers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/containerreadings"
	sdk "github.com/block/proto-fleet/server/sdk/v1"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Store interface {
	ListContainers(ctx context.Context, params sqlc.ListContainersParams) ([]sqlc.ListContainersRow, error)
}
type Service struct{ store Store }

func NewService(store Store) *Service { return &Service{store: store} }

// List authorizes each current site before exposing identity or telemetry.
func (s *Service) List(ctx context.Context, orgID int64, siteIDs []int64, unassigned bool, canRead func(*int64) (bool, error)) ([]*pb.Container, error) {
	return s.list(ctx, orgID, siteIDs, unassigned, nil, canRead)
}

// Get uses the same current-site authorization and projection as the list.
// A denied identifier is indistinguishable from a missing one.
func (s *Service) Get(ctx context.Context, orgID int64, identifier string, canRead func(*int64) (bool, error)) (*pb.Container, error) {
	// Fast path only: the filter below never matches an empty identifier either.
	if identifier == "" {
		return nil, fleeterror.NewNotFoundError("container not found")
	}
	rows, err := s.list(ctx, orgID, nil, false, []string{identifier}, canRead)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0].DeviceIdentifier != identifier {
		return nil, fleeterror.NewNotFoundError("container not found")
	}
	return rows[0], nil
}

// list filters to identifiers when given; nil or empty lists every container in scope.
func (s *Service) list(ctx context.Context, orgID int64, siteIDs []int64, unassigned bool, identifiers []string, canRead func(*int64) (bool, error)) ([]*pb.Container, error) {
	rows, err := s.store.ListContainers(ctx, sqlc.ListContainersParams{OrgID: orgID, SiteIds: siteIDs, IncludeUnassigned: unassigned, DeviceIdentifiers: identifiers})
	if err != nil {
		return nil, err
	}
	return Project(rows, canRead)
}

// Project builds container cards from an authorized, bounded set of observations.
func Project(rows []sqlc.ListContainersRow, canRead func(*int64) (bool, error)) ([]*pb.Container, error) {
	result := make([]*pb.Container, 0, len(rows))
	now := time.Now()
	for _, r := range rows {
		var siteID *int64
		if r.SiteID.Valid {
			siteID = &r.SiteID.Int64
		}
		allowed, err := canRead(siteID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		c := &pb.Container{Id: r.ID, DeviceIdentifier: r.DeviceIdentifier, Name: r.Name, Stale: true}
		if c.Name == "" {
			c.Name = "Container " + r.SerialNumber.String
		}
		if siteID != nil {
			c.Site = &commonpb.ResourceRef{Id: *siteID, Label: r.SiteName}
		}
		if r.ObservedAt.Valid {
			if err := json.Unmarshal(r.TankHealth, &c.Tanks); err != nil {
				return nil, fmt.Errorf("decode container tank health: %w", err)
			}
			c.ObservedAt = timestamppb.New(r.ObservedAt.Time)
			c.Stale = now.Sub(r.ObservedAt.Time) > 2*time.Minute || r.ObservedAt.Time.After(now.Add(5*time.Second))
		}
		if r.MetricsAt.Unix() > 0 {
			c.MetricsAt = timestamppb.New(r.MetricsAt)
		}
		metricsFresh := now.Sub(r.MetricsAt) <= 2*time.Minute && !r.MetricsAt.After(now.Add(5*time.Second))
		if metricsFresh && r.HashRateHs.Valid {
			c.HashrateHs = &r.HashRateHs.Float64
		}
		if metricsFresh && r.PowerW.Valid {
			c.PowerW = &r.PowerW.Float64
		}
		if metricsFresh && r.TempC.Valid {
			c.TemperatureC = &r.TempC.Float64
		}
		if metricsFresh {
			if r.EfficiencyJh.Valid {
				c.EfficiencyJh = &r.EfficiencyJh.Float64
			}
			if r.MinTemperatureC.Valid {
				c.MinTemperatureC = &r.MinTemperatureC.Float64
			}
			if r.MaxTemperatureC.Valid {
				c.MaxTemperatureC = &r.MaxTemperatureC.Float64
			}
		}
		if r.Coverage.Valid {
			var coverage sdk.ContainerRollup
			if err := json.Unmarshal(r.Coverage.RawMessage, &coverage); err != nil {
				return nil, fmt.Errorf("decode container coverage: %w", err)
			}
			c.Coverage = containerreadings.Coverage(&coverage)
			if metricsFresh && !c.Stale {
				c.UnlocatedHashboards = &coverage.UnlocatedHashboards
			}
		}
		if err := projectEquipment(c, r.Fans, r.Controls, now); err != nil {
			return nil, err
		}
		if err := projectPlant(c, r.Plant, now); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}
