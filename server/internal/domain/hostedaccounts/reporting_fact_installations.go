package hostedaccounts

import (
	"context"
	"fmt"
	"slices"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
)

// Board ownership is established by reviewed coverage; physical membership uses
// retained installation timestamps, compared to the exact original mapping.
func validateReportingModules(ctx context.Context, q sqlc.Querier, org int64, input reporting.Inputs) error {
	if len(input.ModuleCoverage) == 0 {
		return nil
	}
	boards := map[string]int64{}
	for _, c := range input.Coverage {
		boards[c.ID] = c.AssetID
	}
	args := sqlc.ListReportingModuleInstallationCandidatesParams{OrganizationID: org, WindowStart: input.ModuleCoverage[0].Start, WindowEnd: input.ModuleCoverage[0].End, PageLimit: MaxReportingFactPageSize}
	ids := make([][]int64, len(input.ModuleCoverage))
	selected := make([][]sqlc.HashboardInstallation, len(input.ModuleCoverage))
	for n, m := range input.ModuleCoverage {
		for _, id := range m.BoardCoverageIDs {
			ids[n] = append(ids[n], boards[id])
		}
		args.BoardIds = append(args.BoardIds, ids[n]...)
		args.ModuleIds = append(args.ModuleIds, m.ModuleID)
		if m.Start.Before(args.WindowStart) {
			args.WindowStart = m.Start
		}
		if m.End.After(args.WindowEnd) {
			args.WindowEnd = m.End
		}
	}
	slices.Sort(args.BoardIds)
	args.BoardIds = slices.Compact(args.BoardIds)
	slices.Sort(args.ModuleIds)
	args.ModuleIds = slices.Compact(args.ModuleIds)
	visit := func(fn func(sqlc.HashboardInstallation) error) error {
		args.AfterID = 0
		for {
			rows, e := q.ListReportingModuleInstallationCandidates(ctx, args)
			if e != nil {
				return e
			}
			for _, r := range rows {
				if e := ctx.Err(); e != nil {
					return fmt.Errorf("validate reporting installations: %w", e)
				}
				if e = fn(r); e != nil {
					return e
				}
				args.AfterID = r.ID
			}
			if len(rows) < MaxReportingFactPageSize {
				return nil
			}
		}
	}
	e := visit(func(i sqlc.HashboardInstallation) error {
		for n, m := range input.ModuleCoverage {
			if !slices.Contains(ids[n], i.HashboardID) || i.ModuleID != m.ModuleID || i.DeviceID != m.ContainerID || !i.SiteID.Valid || i.SiteID.Int64 != m.SiteID || i.InstalledAt.After(m.Start) || (i.RemovedAt.Valid && i.RemovedAt.Time.Before(m.End)) {
				continue
			}
			for _, chosen := range selected[n] {
				if i.HashboardID == chosen.HashboardID || i.Position == chosen.Position {
					return fleeterror.NewInvalidArgumentError("module coverage requires unambiguous historical board installations throughout its window")
				}
			}
			selected[n] = append(selected[n], i)
		}
		return nil
	})
	if e != nil {
		return e
	}
	for n := range input.ModuleCoverage {
		if len(selected[n]) != len(ids[n]) {
			return fleeterror.NewInvalidArgumentError("module coverage requires unambiguous historical board installations throughout its window")
		}
	}
	e = visit(func(i sqlc.HashboardInstallation) error {
		end := i.RemovedAt.Time
		if !i.RemovedAt.Valid {
			end = reportingThrough(end)
		}
		for n, m := range input.ModuleCoverage {
			if !reportingOverlap(m.Start, m.End, i.InstalledAt, end) {
				continue
			}
			for _, chosen := range selected[n] {
				if i.ID != chosen.ID && (i.HashboardID == chosen.HashboardID || (i.ModuleID == chosen.ModuleID && i.Position == chosen.Position)) {
					return fleeterror.NewInvalidArgumentErrorf("module coverage %q requires unambiguous historical board installations throughout its window", m.ID)
				}
			}
		}
		return nil
	})
	if e != nil {
		return fmt.Errorf("validate module installations: %w", e)
	}
	return nil
}
