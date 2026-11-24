package hostedaccounts

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/block/proto-fleet/server/internal/domain/sites"
)

// reportingSettingsSource is the reference recorded for reporting inputs that
// claim no reviewed evidence: assumed defaults and plain settings changes.
const reportingSettingsSource = "Fleet reporting settings"

// assumedReportingConfiguration returns the configuration Fleet uses for a
// client without saved reporting inputs: the calendar year, in the timezone
// shared by every site holding the client's open allocations. It records no
// contract evidence, so gated figures stay provisional. ok is false when those
// sites do not resolve to one timezone, or when the actor cannot read one of
// them; a person must then choose it.
func assumedReportingConfiguration(ctx context.Context, q sqlc.Querier, a reportingAccess) (config reporting.Configuration, ok bool, err error) {
	rows, err := q.ListReportingAllocatedSiteLocations(ctx, sqlc.ListReportingAllocatedSiteLocationsParams{OrganizationID: a.account.OrganizationID, ClientID: a.account.ID})
	if err != nil {
		return config, false, err
	}
	timezone := ""
	for _, row := range rows {
		if !a.sitesAllowed([]int64{row.ID}) {
			return config, false, nil
		}
		zone := strings.TrimSpace(row.Timezone.String)
		if zone == "" {
			zone = sites.InferTimezone(row.Country, row.LocationState.String)
		}
		if zone == "" || (timezone != "" && zone != timezone) {
			return config, false, nil
		}
		timezone = zone
	}
	if timezone == "" {
		return config, false, nil
	}
	config = reporting.Configuration{Timezone: timezone, ReportingYearMonth: 1, ReportingYearDay: 1}
	if err := reporting.ValidateConfigurationContext(ctx, config); err != nil {
		if ctx.Err() != nil {
			return reporting.Configuration{}, false, fmt.Errorf("assume reporting configuration: %w", err)
		}
		// A site can store a timezone the reporting calendar does not accept.
		return reporting.Configuration{}, false, nil
	}
	return config, true, nil
}

// initializeAssumedReportingInputs saves the assumed configuration as the
// account's first input revision and returns it locked for report preparation.
// Revision zero does not pin a timezone, so a period prepared against a
// different assumption is a conflict the caller resolves by refreshing.
func initializeAssumedReportingInputs(ctx context.Context, q sqlc.Querier, a reportingAccess, periodTimezone string) (sqlc.HostedReportingInput, error) {
	var zero sqlc.HostedReportingInput
	config, ok, err := assumedReportingConfiguration(ctx, q, a)
	if err != nil {
		return zero, err
	}
	if !ok {
		return zero, fleeterror.NewFailedPreconditionError("choose a reporting timezone in reporting settings before creating a report")
	}
	if config.Timezone != periodTimezone {
		return zero, reportingConflict()
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		return zero, fmt.Errorf("encode reporting configuration: %w", err)
	}
	inserted, err := q.InitializeReportingInputs(ctx, sqlc.InitializeReportingInputsParams{OrganizationID: a.account.OrganizationID, AccountID: a.account.ID, UpdatedBy: a.actor.ID, SourceRef: reportingSettingsSource, Configuration: configJSON})
	if err != nil {
		return zero, err
	}
	if inserted != 1 {
		return zero, reportingConflict()
	}
	if err = audit(ctx, q, a.actor, a.account, "save_reporting_inputs", map[string]any{"revision": 1, "previous_revision": 0, "changed_sections": []string{"configuration"}, "register_counts": map[reporting.FactKind]int64{}, "source_ref": reportingSettingsSource, "assumed": true}); err != nil {
		return zero, err
	}
	return q.LockReportingInputsForReport(ctx, sqlc.LockReportingInputsForReportParams{OrganizationID: a.account.OrganizationID, AccountID: a.account.ID})
}
