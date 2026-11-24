package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
)

type CutoverReport struct {
	Inventory sqlc.GetPoolCutoverInventoryRow  `json:"inventory"`
	Gaps      []poolconfigurations.ImportIssue `json:"unresolved"`
	Frozen    bool                             `json:"writers_frozen"`
	Complete  bool                             `json:"complete"`
}

// FreezeCutover runs after import, with application writers stopped. Require the
// same persisted completeness as completion: an importer can skip equipment or
// report gaps that need normal assignment/cleanup, which freezing would disable.
// Source locks also make previously admitted writers finish before this check.
func FreezeCutover(ctx context.Context, q sqlc.Querier, orgID int64) error {
	if err := lockCutoverSources(ctx, q, orgID); err != nil {
		return err
	}
	report, err := InspectCutover(ctx, q, orgID)
	if err != nil || report.Complete {
		return err
	}
	if len(report.Gaps) != 0 {
		gap := report.Gaps[0]
		return fmt.Errorf("pool freeze has %d unresolved records; finish import and resolve all gaps before freezing: %s %s", len(report.Gaps), gap.Source.Kind, gap.Source.ID)
	}
	return q.FreezePoolConfigurationWriters(ctx, orgID)
}

// InspectCutover is read-only and contains no credentials. Hardware observations
// are deliberately not inferred from matching library URLs or operation status.
func InspectCutover(ctx context.Context, q sqlc.Querier, orgID int64) (CutoverReport, error) {
	var report CutoverReport
	if orgID <= 0 {
		return report, fmt.Errorf("pool cutover requires a positive organization ID")
	}
	var err error
	report.Inventory, err = q.GetPoolCutoverInventory(ctx, orgID)
	if err != nil {
		return report, err
	}
	report.Complete, err = q.HasPoolConfigurationCutover(ctx, orgID)
	if err != nil {
		return report, err
	}
	report.Frozen, err = q.PoolConfigurationWritersFrozen(ctx, orgID)
	if err != nil || report.Complete {
		return report, err
	}
	gaps, err := q.ListPoolCutoverGaps(ctx, orgID)
	if err != nil {
		return report, err
	}
	for _, gap := range gaps {
		report.Gaps = append(report.Gaps, poolconfigurations.ImportIssue{Source: poolconfigurations.ImportSource{Kind: gap.Kind, ID: gap.SourceID}, Reason: gap.Reason})
	}
	work, err := q.ListUnsettledPoolAdoptionWork(ctx, orgID)
	if err != nil {
		return report, err
	}
	for _, item := range work {
		if devicewrite.MayAffectPools(item.OperationKind, item.CommandType) {
			report.Gaps = append(report.Gaps, poolconfigurations.ImportIssue{Source: poolconfigurations.ImportSource{Kind: "unfinished_work", ID: strconv.FormatInt(item.DeviceID, 10)}, Reason: unsettledPoolReason})
		}
	}
	devices, err := q.ListPoolDeviceImportSources(ctx, orgID)
	if err != nil {
		return report, err
	}
	for _, device := range devices {
		report.Gaps = append(report.Gaps, poolconfigurations.ImportIssue{Source: poolconfigurations.ImportSource{Kind: "unmanaged_device", ID: strconv.FormatInt(device.ID, 10)}, Reason: "Complete authoritative device settings have not been adopted."})
	}
	sources, err := q.ListHostedPoolImportSources(ctx, orgID)
	if err != nil {
		return report, err
	}
	for _, source := range sources {
		_, err := q.GetPoolConfigurationImport(ctx, sqlc.GetPoolConfigurationImportParams{OrganizationID: orgID, AccountID: sql.NullInt64{Int64: source.OwnerAccountID, Valid: source.OwnerAccountID != 0}, SourceKind: "hosted_revision", SourceID: source.RevisionID, SourceVersion: strconv.FormatInt(source.Number, 10)})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return report, err
		}
		if err != nil {
			report.Gaps = append(report.Gaps, poolconfigurations.ImportIssue{Source: poolconfigurations.ImportSource{Kind: "hosted_revision", ID: source.RevisionID}, Reason: "Required hosted revision has not been imported."})
		}
	}
	return report, nil
}

// CompleteCutover must run in a bounded maintenance transaction with dispatchers
// stopped. Locks and writer triggers fence races; unresolved records never make
// the marker visible. Earlier successful imports remain safe to rerun.
func CompleteCutover(ctx context.Context, q sqlc.Querier, orgID int64) (CutoverReport, error) {
	if err := lockCutoverSources(ctx, q, orgID); err != nil {
		return CutoverReport{}, err
	}
	report, err := InspectCutover(ctx, q, orgID)
	if err != nil || report.Complete {
		return report, err
	}
	if !report.Frozen {
		return report, fmt.Errorf("freeze legacy writers before completing pool cutover")
	}
	if len(report.Gaps) != 0 {
		return report, fmt.Errorf("pool cutover has %d unresolved records", len(report.Gaps))
	}
	report.Complete = true
	report.Frozen = false
	encoded, err := json.Marshal(report)
	if err != nil {
		return report, fmt.Errorf("encode cutover report: %w", err)
	}
	return report, q.RecordPoolConfigurationCutover(ctx, sqlc.RecordPoolConfigurationCutoverParams{OrganizationID: orgID, Report: encoded})
}

func lockCutoverSources(ctx context.Context, q sqlc.Querier, orgID int64) error {
	if err := q.BoundPoolConfigurationImportLocks(ctx); err != nil {
		return err
	}
	if _, err := q.LockPoolConfigurationImportOrganization(ctx, orgID); err != nil {
		return err
	}
	if err := q.LockPoolAssignmentAdoptionSources(ctx); err != nil {
		return err
	}
	return q.LockLegacyPoolLibraryForImport(ctx)
}
