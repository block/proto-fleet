package sites

import (
	"context"
	"math"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/sites/models"
)

type ContainerStatsReader interface {
	ListContainerSiteStats(ctx context.Context, params sqlc.ListContainerSiteStatsParams) ([]sqlc.ListContainerSiteStatsRow, error)
}

func (s *Service) WithContainerStats(reader ContainerStatsReader) *Service {
	s.containerStats = reader
	return s
}

func (s *Service) addContainerStats(ctx context.Context, orgID int64, targets map[int64]*models.FleetListStats) error {
	if s.containerStats == nil || len(targets) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(targets))
	for id := range targets {
		ids = append(ids, id)
	}
	rows, err := s.containerStats.ListContainerSiteStats(ctx, sqlc.ListContainerSiteStatsParams{OrgID: orgID, SiteIds: ids})
	if err != nil {
		return err
	}
	for _, row := range rows {
		stats := targets[row.SiteID]
		if stats == nil {
			continue
		}
		efficiencyCount := stats.EfficiencyReportingCount + row.EfficiencyReportingCount
		if efficiencyCount > 0 {
			stats.AvgEfficiencyJth = (stats.AvgEfficiencyJth*float64(stats.EfficiencyReportingCount) + row.AvgEfficiencyJth*float64(row.EfficiencyReportingCount)) / float64(efficiencyCount)
		}
		if row.TemperatureReportingCount > 0 {
			if stats.TemperatureReportingCount == 0 {
				stats.MinTemperatureC = row.MinTemperatureC
				stats.MaxTemperatureC = row.MaxTemperatureC
			} else {
				stats.MinTemperatureC = math.Min(stats.MinTemperatureC, row.MinTemperatureC)
				stats.MaxTemperatureC = math.Max(stats.MaxTemperatureC, row.MaxTemperatureC)
			}
		}
		stats.BuildingCount += row.ContainerCount
		stats.RackCount += row.TankCount
		stats.DeviceCount += row.ModuleCount
		stats.HashingCount += row.HashingCount
		stats.BrokenCount += row.BrokenCount
		stats.OfflineCount += row.OfflineCount
		stats.SleepingCount += row.SleepingCount
		stats.ReportingCount += row.ReportingCount
		stats.HashrateReportingCount += row.HashrateReportingCount
		stats.PowerReportingCount += row.PowerReportingCount
		stats.EfficiencyReportingCount = efficiencyCount
		stats.TemperatureReportingCount += row.TemperatureReportingCount
		stats.TotalHashrateThs += row.TotalHashrateThs
		stats.TotalPowerKw += row.TotalPowerKw
	}
	return nil
}
