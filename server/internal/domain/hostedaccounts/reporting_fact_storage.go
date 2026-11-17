package hostedaccounts

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/jackc/pgx/v5/pgconn"
)

type ReportingInputsMetadata struct {
	Revision       int64                        `json:"revision"`
	Configuration  reporting.Configuration      `json:"configuration"`
	SourceRef      string                       `json:"sourceRef"`
	UpdatedBy      int64                        `json:"updatedBy"`
	UpdatedAt      time.Time                    `json:"updatedAt"`
	SiteIDs        []int64                      `json:"siteIds"`
	RegisterCounts map[reporting.FactKind]int64 `json:"registerCounts"`
}

func reportingInputsMetadata(ctx context.Context, q sqlc.Querier, row sqlc.HostedReportingInput) (ReportingInputsMetadata, error) {
	out := ReportingInputsMetadata{Revision: row.Revision, SourceRef: row.SourceRef, UpdatedBy: row.UpdatedBy, UpdatedAt: row.UpdatedAt, SiteIDs: row.SiteIds, RegisterCounts: map[reporting.FactKind]int64{}}
	if e := json.Unmarshal(row.Configuration, &out.Configuration); e != nil {
		return out, fmt.Errorf("decode reporting configuration: %w", e)
	}
	counts, e := q.CountReportingRegisters(ctx, sqlc.CountReportingRegistersParams{OrganizationID: row.OrganizationID, AccountID: row.AccountID})
	if e != nil {
		return out, e
	}
	for _, c := range counts {
		out.RegisterCounts[reporting.FactKind(c.Kind)] = c.FactCount
	}
	return out, nil
}

func (s *Service) ReportingInputsMetadata(ctx context.Context, actorID, orgID int64, accountID string) (ReportingInputsMetadata, error) {
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (ReportingInputsMetadata, error) {
		var zero ReportingInputsMetadata
		a, e := reportingHostReadAdmission(ctx, q, actorID, orgID, accountID, "read")
		if e != nil {
			return zero, e
		}
		row, e := q.LockReportingInputsForRead(ctx, sqlc.LockReportingInputsForReadParams{OrganizationID: orgID, AccountID: a.account.ID})
		if errors.Is(e, sql.ErrNoRows) {
			return zero, fleeterror.NewNotFoundError("reporting inputs not found")
		}
		if e != nil {
			return zero, e
		}
		if !a.hostSitesAllowed(row.SiteIds) {
			return zero, denied()
		}
		return reportingInputsMetadata(ctx, q, row)
	})
}

func (s *Service) MutateReportingInputs(ctx context.Context, actorID, orgID int64, accountID string, expected int64, source string, configuration *reporting.Configuration, mutations []reporting.FactMutation) (ReportingInputsMetadata, error) {
	var zero ReportingInputsMetadata
	if expected < 0 || strings.TrimSpace(source) == "" || len(source) > 2000 {
		return zero, fleeterror.NewInvalidArgumentError("input source evidence and revision are required")
	}
	ctx, cancel := context.WithTimeout(ctx, miningTransactionTimeout)
	defer cancel()
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (ReportingInputsMetadata, error) {
		a, e := reportingHostAdmissionMode(ctx, q, actorID, orgID, accountID, "create", true)
		if e != nil {
			return zero, e
		}
		previous, e := q.LockReportingInputsForUpdate(ctx, sqlc.LockReportingInputsForUpdateParams{OrganizationID: orgID, AccountID: a.account.ID})
		absent := errors.Is(e, sql.ErrNoRows)
		if e != nil && !absent {
			return zero, e
		}
		if !a.hostSitesAllowed(previous.SiteIds) {
			return zero, denied()
		}
		if previous.Revision != expected {
			return zero, reportingConflict()
		}
		if len(mutations) == 0 && configuration == nil {
			return zero, fleeterror.NewInvalidArgumentError("at least one reporting input operation is required")
		}
		prepared, e := reporting.PrepareFactMutations(ctx, mutations)
		if e != nil {
			return zero, fleeterror.NewInvalidArgumentError(e.Error())
		}
		var config reporting.Configuration
		if !absent {
			if e = json.Unmarshal(previous.Configuration, &config); e != nil {
				return zero, fmt.Errorf("decode reporting configuration: %w", e)
			}
		}
		priorConfig := config
		if configuration != nil {
			config = *configuration
		}
		if e = reporting.ValidateConfigurationContext(ctx, config); e != nil {
			return zero, fleeterror.NewInvalidArgumentError(e.Error())
		}
		if !sameReportingRepresentatives(priorConfig.Representatives, config.Representatives) {
			allowed, err := reportingRepresentativeAuthority(ctx, q, actorID, orgID, accountID)
			if err != nil {
				return zero, err
			}
			if !allowed {
				return zero, fleeterror.NewForbiddenError("managing report recipients requires account team-management access")
			}
		}
		configJSON, e := json.Marshal(config)
		if e != nil {
			return zero, fmt.Errorf("encode reporting configuration: %w", e)
		}
		if len(configJSON) > reporting.MaxReportingConfigurationBytes {
			return zero, fleeterror.NewInvalidArgumentError("reporting configuration exceeds byte budget")
		}

		old := make([]reporting.Fact, 0, len(prepared))
		unchanged := make(map[reporting.FactKey]bool, len(prepared))
		next := make([]reporting.Fact, 0, len(prepared))
		for _, m := range prepared {
			row, e := q.GetReportingFact(ctx, sqlc.GetReportingFactParams{OrganizationID: orgID, AccountID: a.account.ID, Kind: string(m.Kind), FactID: m.ID})
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return zero, e
			}
			if m.Delete && errors.Is(e, sql.ErrNoRows) {
				return zero, fleeterror.NewInvalidArgumentErrorf("cannot delete absent %s %q", m.Kind, m.ID)
			}
			if e == nil {
				f, _, e := reporting.DecodeFact(m.Kind, row.Payload)
				if e != nil {
					return zero, e
				}
				unchanged[reporting.FactKey{Kind: m.Kind, ID: m.ID}] = !m.Delete && bytes.Equal(f.Payload, m.Payload)
				// Keep only old selectors and scope. Deleting many formerly large
				// rows must not retain all of their payloads in this request.
				f.Payload = nil
				f.BoardCoverageIDs = nil
				old = append(old, f)
			}
			if !m.Delete {
				f, _, e := reporting.DecodeFact(m.Kind, m.Payload)
				if e != nil {
					return zero, e
				}
				next = append(next, f)
			}
		}
		// Delete-only batches repair drift incrementally. Keep authority and
		// final relationships strict while skipping mutable witnesses, account
		// site policy and representative checks that would prevent cleanup.
		deleteOnly := configuration == nil && len(next) == 0
		if !deleteOnly {
			if e = lockReportingFactDevices(ctx, q, a, next); e != nil {
				return zero, e
			}
		}
		if absent {
			inserted, e := q.InitializeReportingInputs(ctx, sqlc.InitializeReportingInputsParams{OrganizationID: orgID, AccountID: a.account.ID, UpdatedBy: actorID, SourceRef: strings.TrimSpace(source), Configuration: configJSON})
			if e != nil {
				return zero, e
			}
			if inserted != 1 {
				return zero, reportingConflict()
			}
		}
		for _, m := range prepared {
			if m.Delete {
				if _, e = q.DeleteReportingFact(ctx, sqlc.DeleteReportingFactParams{OrganizationID: orgID, AccountID: a.account.ID, Kind: string(m.Kind), FactID: m.ID}); e != nil {
					return zero, e
				}
			}
		}
		for _, f := range next {
			if e = putReportingFact(ctx, q, orgID, a.account.ID, f); e != nil {
				return zero, e
			}
		}
		if !deleteOnly {
			if e = validateReportingExternalWitnesses(ctx, q, a); e != nil {
				return zero, e
			}
		}
		if e = validateReportingFinalFacts(ctx, q, a, config, old, next); e != nil {
			return zero, e
		}

		if !deleteOnly {
			for _, r := range config.Representatives {
				if _, e = q.FindHostedAccountMemberByUUID(ctx, sqlc.FindHostedAccountMemberByUUIDParams{OrganizationID: orgID, AccountID: a.account.ID, UserID: r.UserID}); e != nil {
					if errors.Is(e, sql.ErrNoRows) {
						return zero, fleeterror.NewInvalidArgumentError("representative must be a current direct account teammate")
					}
					return zero, e
				}
			}
		}
		deltas := map[int64]int64{}
		for _, f := range old {
			if f.SiteID > 0 {
				deltas[f.SiteID]--
			}
		}
		for _, f := range next {
			if f.SiteID > 0 {
				deltas[f.SiteID]++
			}
		}
		for site, delta := range deltas {
			if delta != 0 {
				if adjusted, adjustErr := q.AdjustReportingFactSite(ctx, sqlc.AdjustReportingFactSiteParams{OrganizationID: orgID, AccountID: a.account.ID, SiteID: site, Delta: delta}); adjustErr != nil || adjusted != 1 {
					if adjustErr != nil {
						return zero, adjustErr
					}
					return zero, fmt.Errorf("reporting site count underflow")
				}
			}
		}
		if e = q.DeleteEmptyReportingFactSites(ctx, sqlc.DeleteEmptyReportingFactSitesParams{OrganizationID: orgID, AccountID: a.account.ID}); e != nil {
			return zero, e
		}
		sites, e := q.ListReportingFactSites(ctx, sqlc.ListReportingFactSitesParams{OrganizationID: orgID, AccountID: a.account.ID})
		if e != nil {
			return zero, e
		}
		if sites == nil {
			sites = []int64{}
		}
		if !a.hostSitesAllowed(sites) {
			return zero, denied()
		}
		if schedule := config.Schedule; schedule != nil {
			owner, err := q.GetReportingUser(ctx, sqlc.GetReportingUserParams{UserID: schedule.OwnerUserID, OrganizationID: orgID})
			if err != nil {
				return zero, accessError(err)
			}
			grant, err := q.GetReportingAuthority(ctx, sqlc.GetReportingAuthorityParams{OrganizationID: orgID, AccountID: a.account.ID, UserID: owner})
			if err != nil {
				return zero, accessError(err)
			}
			if !grant.CanCreate {
				return zero, denied()
			}
			for _, site := range sites {
				if !grant.AllSites && !slices.Contains(grant.SiteIds, site) {
					return zero, denied()
				}
			}
		}
		row, e := q.UpdateReportingInputs(ctx, sqlc.UpdateReportingInputsParams{OrganizationID: orgID, AccountID: a.account.ID, Revision: expected + 1, UpdatedBy: actorID, SourceRef: strings.TrimSpace(source), Configuration: configJSON, SiteIds: sites})
		if e != nil {
			return zero, e
		}
		out, e := reportingInputsMetadata(ctx, q, row)
		if e != nil {
			return zero, e
		}
		sections := []string{}
		if configuration != nil && (absent || !reportingConfigurationsEqual(priorConfig, config)) {
			sections = append(sections, "configuration")
		}
		for _, m := range prepared {
			if !unchanged[reporting.FactKey{Kind: m.Kind, ID: m.ID}] {
				sections = append(sections, reportingRegisterSection(m.Kind))
			}
		}
		slices.Sort(sections)
		sections = slices.Compact(sections)
		if e = audit(ctx, q, a.actor, a.account, "save_reporting_inputs", map[string]any{"revision": out.Revision, "previous_revision": expected, "changed_sections": sections, "register_counts": out.RegisterCounts, "source_ref": strings.TrimSpace(source)}); e != nil {
			return zero, e
		}
		return out, nil
	})
}

func reportingRegisterSection(k reporting.FactKind) string {
	switch k {
	case reporting.FactOrder:
		return "orders"
	case reporting.FactCoverage:
		return "coverage"
	case reporting.FactModuleCoverage:
		return "moduleCoverage"
	case reporting.FactMeter:
		return "meters"
	case reporting.FactFee:
		return "fees"
	case reporting.FactIncident:
		return "incidents"
	case reporting.FactHashrate:
		return "hashrates"
	case reporting.FactPoolObservation:
		return "poolObservations"
	case reporting.FactObligation:
		return "obligations"
	case reporting.FactExternalResult:
		return "externalResults"
	}
	return string(k)
}

func reportingNullTime(t time.Time) sql.NullTime { return sql.NullTime{Time: t, Valid: !t.IsZero()} }
func reportingNullID(id int64) sql.NullInt64     { return sql.NullInt64{Int64: id, Valid: id != 0} }
func reportingNullText(s string) sql.NullString  { return sql.NullString{String: s, Valid: s != ""} }
func putReportingFact(ctx context.Context, q sqlc.Querier, org, account int64, f reporting.Fact) error {
	boards := f.BoardCoverageIDs
	if boards == nil {
		boards = []string{}
	}
	nanos := int32(f.Start.Nanosecond()) // #nosec G115 -- time.Time.Nanosecond is always in [0, 999999999].
	return q.PutReportingFact(ctx, sqlc.PutReportingFactParams{OrganizationID: org, AccountID: account, Kind: string(f.Kind), FactID: f.ID, StartAt: f.Start, EndAt: reportingNullTime(f.End), StartSeconds: f.Start.Unix(), StartNanos: nanos, ReceivedAt: reportingNullTime(f.ReceivedAt), SiteID: reportingNullID(f.SiteID), ContainerID: reportingNullID(f.ContainerID), AssetKind: sql.NullInt32{Int32: f.AssetKind, Valid: f.AssetKind != 0}, AssetID: reportingNullID(f.AssetID), OrderID: reportingNullText(f.OrderID), MeterID: reportingNullText(f.MeterID), AllocationIntervalID: reportingNullID(f.AllocationIntervalID), EpisodeID: reportingNullText(f.EpisodeID), BoardCoverageIds: boards, Payload: f.Payload})
}

// lockReportingFactDevices merges the complete saved device stream with bounded
// proposed references so every NOWAIT fence is taken in globally sorted order.
func lockReportingFactDevices(ctx context.Context, q sqlc.Querier, a reportingAccess, proposed []reporting.Fact) error {
	ids := []int64{}
	for _, f := range proposed {
		if f.ContainerID > 0 {
			ids = append(ids, f.ContainerID)
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	batch := []int64{}
	last := int64(0)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		rows, e := q.LockReportingDevices(ctx, sqlc.LockReportingDevicesParams{Ids: batch, OrgID: a.account.OrganizationID})
		if e != nil {
			var pg *pgconn.PgError
			if errors.As(e, &pg) && pg.Code == "55P03" {
				return reportingConflict()
			}
			return e
		}
		if len(rows) != len(batch) {
			return fleeterror.NewInvalidArgumentError("reporting container is outside the evidenced organization and site")
		}
		batch = batch[:0]
		return nil
	}
	add := func(id int64) error {
		if id == last {
			return nil
		}
		last = id
		batch = append(batch, id)
		if len(batch) == 200 {
			return flush()
		}
		return nil
	}
	after := int64(0)
	i := 0
	for {
		rows, e := q.ListReportingFactDevices(ctx, sqlc.ListReportingFactDevicesParams{OrganizationID: a.account.OrganizationID, AccountID: a.account.ID, AfterID: after, PageLimit: 200})
		if e != nil {
			return e
		}
		for _, id := range rows {
			for i < len(ids) && ids[i] <= id {
				if e = add(ids[i]); e != nil {
					return e
				}
				i++
			}
			if e = add(id); e != nil {
				return e
			}
			after = id
		}
		if len(rows) < 200 {
			break
		}
	}
	for ; i < len(ids); i++ {
		if e := add(ids[i]); e != nil {
			return e
		}
	}
	return flush()
}

type reportingFactMetadata struct {
	rowID int64
	kind  reporting.FactKind
	id    string
	bytes int64
}

func reportingFactPrefix(meta []reportingFactMetadata) []reportingFactMetadata {
	// Reserve the cursor/envelope once. JSON escaping can expand text sixfold,
	// and the derived projection repeats a subset of the payload's fields.
	// The page encoder still checks the actual wire size before returning it.
	budget := int64(MaxReportingFactPageBytes - 32768)
	n := 0
	for _, m := range meta {
		cost := 12*m.bytes + 1024
		if n > 0 && cost > budget {
			break
		}
		n++
		budget -= cost
	}
	return meta[:n]
}
func reportingFactPayloads(ctx context.Context, q sqlc.Querier, org, account int64, meta []reportingFactMetadata) ([]reporting.Fact, error) {
	ids := make([]int64, len(meta))
	for i, m := range meta {
		ids[i] = m.rowID
	}
	rows, e := q.GetReportingFactPayloads(ctx, sqlc.GetReportingFactPayloadsParams{OrganizationID: org, AccountID: account, RowIds: ids})
	if e != nil {
		return nil, e
	}
	if len(rows) != len(meta) {
		return nil, fmt.Errorf("reporting fact changed under revision lock")
	}
	byID := map[int64]reporting.Fact{}
	for _, r := range rows {
		f, _, e := reporting.DecodeFact(reporting.FactKind(r.Kind), r.Payload)
		if e != nil {
			return nil, e
		}
		if f.ID != r.FactID {
			return nil, fmt.Errorf("stored reporting identity mismatch")
		}
		byID[r.RowID.Int64] = f
	}
	out := make([]reporting.Fact, 0, len(meta))
	for _, m := range meta {
		out = append(out, byID[m.rowID])
	}
	return out, nil
}

func reportingConfigurationsEqual(a, b reporting.Configuration) bool {
	normalize := func(c reporting.Configuration) reporting.Configuration {
		if len(c.Registers) == 0 {
			c.Registers = nil
		}
		if len(c.Holidays) == 0 {
			c.Holidays = nil
		}
		if len(c.Prerequisites) == 0 {
			c.Prerequisites = nil
		}
		if len(c.Sources) == 0 {
			c.Sources = nil
		}
		if len(c.Representatives) == 0 {
			c.Representatives = nil
		}
		return c
	}
	return reflect.DeepEqual(normalize(a), normalize(b))
}
