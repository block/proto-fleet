package hostedaccounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"slices"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
)

func reportingOverlap(a, b, c, d time.Time) bool {
	return (b.IsZero() || c.Before(b)) && (d.IsZero() || a.Before(d))
}
func reportingThrough(end time.Time) time.Time {
	if end.IsZero() {
		return time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	}
	return end
}

func reportingFactReferences(ctx context.Context, q sqlc.Querier, a reportingAccess, keys []reporting.FactKey) (map[reporting.FactKey]any, error) {
	values := map[reporting.FactKey]any{}
	seen := map[reporting.FactKey]bool{}
	unique := make([]reporting.FactKey, 0, len(keys))
	for _, key := range keys {
		if !seen[key] {
			unique = append(unique, key)
			seen[key] = true
		}
	}
	if len(unique) == 0 {
		return values, nil
	}
	payload, e := json.Marshal(unique)
	if e != nil {
		return nil, fmt.Errorf("encode reporting reference keys: %w", e)
	}
	rows, e := q.ListReportingReferenceMetadata(ctx, sqlc.ListReportingReferenceMetadataParams{OrganizationID: a.account.OrganizationID, AccountID: a.account.ID, Keys: payload})
	if e != nil {
		return nil, e
	}
	meta := make([]reportingFactMetadata, 0, len(rows))
	for _, r := range rows {
		meta = append(meta, reportingFactMetadata{r.RowID.Int64, reporting.FactKind(r.Kind), r.FactID, r.PayloadBytes.Int64})
	}
	for len(meta) > 0 {
		page := reportingFactPrefix(meta)
		facts, e := reportingFactPayloads(ctx, q, a.account.OrganizationID, a.account.ID, page)
		if e != nil {
			return nil, e
		}
		for _, f := range facts {
			_, value, e := reporting.DecodeFact(f.Kind, f.Payload)
			if e != nil {
				return nil, e
			}
			values[reporting.FactKey{Kind: f.Kind, ID: f.ID}] = value
		}
		meta = meta[len(page):]
	}
	return values, nil
}

func validateReportingFinalFacts(ctx context.Context, q sqlc.Querier, a reportingAccess, config reporting.Configuration, old, next []reporting.Fact) error {
	args := sqlc.ListReportingValidationMetadataParams{OrganizationID: a.account.OrganizationID, AccountID: a.account.ID, ChangedRows: []int64{}, OrderIds: []string{}, BoardIds: []string{}, Assets: json.RawMessage("[]"), PageLimit: 200}
	type asset struct {
		Kind int32 `json:"kind"`
		ID   int64 `json:"id"`
	}
	assets := []asset{}
	seenAssets := map[asset]bool{}
	changed := map[reporting.FactKey]bool{}
	for _, facts := range [][]reporting.Fact{old, next} {
		for _, f := range facts {
			switch f.Kind {
			case reporting.FactOrder:
				args.OrderIds = append(args.OrderIds, f.ID)
			case reporting.FactCoverage:
				args.BoardIds = append(args.BoardIds, f.ID)
				seenAssets[asset{f.AssetKind, f.AssetID}] = true
			case reporting.FactModuleCoverage:
				seenAssets[asset{reporting.AssetKindModule, f.AssetID}] = true
			case reporting.FactMeter, reporting.FactFee, reporting.FactIncident, reporting.FactHashrate, reporting.FactPoolObservation, reporting.FactObligation, reporting.FactExternalResult:
			}
		}
	}
	for key := range seenAssets {
		assets = append(assets, key)
	}
	slices.Sort(args.OrderIds)
	args.OrderIds = slices.Compact(args.OrderIds)
	slices.Sort(args.BoardIds)
	args.BoardIds = slices.Compact(args.BoardIds)
	for _, f := range next {
		changed[reporting.FactKey{Kind: f.Kind, ID: f.ID}] = true
		row, e := q.GetReportingFact(ctx, sqlc.GetReportingFactParams{OrganizationID: a.account.OrganizationID, AccountID: a.account.ID, Kind: string(f.Kind), FactID: f.ID})
		if e != nil {
			return e
		}
		args.ChangedRows = append(args.ChangedRows, row.RowID.Int64)
	}
	var encodeErr error
	args.Assets, encodeErr = json.Marshal(assets)
	if encodeErr != nil {
		return fmt.Errorf("encode reporting dependency selectors: %w", encodeErr)
	}
	if !config.SharedAllocationApproved() {
		has, e := q.HasReportingAllocatedMeters(ctx, sqlc.HasReportingAllocatedMetersParams{OrganizationID: a.account.OrganizationID, AccountID: a.account.ID})
		if e != nil {
			return e
		}
		if has {
			return fleeterror.NewInvalidArgumentError("shared meter requires approved allocation evidence")
		}
	}
	for {
		rows, e := q.ListReportingValidationMetadata(ctx, args)
		if e != nil {
			return e
		}
		if len(rows) == 0 {
			return nil
		}
		meta := make([]reportingFactMetadata, 0, len(rows))
		for _, r := range rows {
			meta = append(meta, reportingFactMetadata{r.RowID.Int64, reporting.FactKind(r.Kind), r.FactID, r.PayloadBytes.Int64})
		}
		meta = reportingFactPrefix(meta)
		facts, e := reportingFactPayloads(ctx, q, a.account.OrganizationID, a.account.ID, meta)
		if e != nil {
			return e
		}
		keys := []reporting.FactKey{}
		for _, f := range facts {
			if f.OrderID != "" {
				keys = append(keys, reporting.FactKey{Kind: reporting.FactOrder, ID: f.OrderID})
			}
			for _, id := range f.BoardCoverageIDs {
				keys = append(keys, reporting.FactKey{Kind: reporting.FactCoverage, ID: id})
			}
		}
		references, e := reportingFactReferences(ctx, q, a, keys)
		if e != nil {
			return e
		}
		support := []reporting.Fact{}
		for _, f := range facts {
			if e := ctx.Err(); e != nil {
				return fmt.Errorf("validate reporting facts: %w", e)
			}
			isChanged := changed[reporting.FactKey{Kind: f.Kind, ID: f.ID}]
			if e = validateReportingFinalFact(ctx, q, a, config, f, references, isChanged); e != nil {
				return fmt.Errorf("%s %q: %w", f.Kind, f.ID, e)
			}
			if isChanged || seenAssets[asset{f.AssetKind, f.AssetID}] {
				support = append(support, f)
			}
		}
		if e = validateReportingCoverageUnions(ctx, q, a, support); e != nil {
			return e
		}
		args.AfterRowID = meta[len(meta)-1].rowID
	}
}

func visitReportingAssetSupport(ctx context.Context, q sqlc.Querier, a reportingAccess, kind int32, asset int64, start, end time.Time, kinds []string, visit func(reporting.Fact) error) error {
	args := sqlc.ListReportingAssetSupportMetadataParams{OrganizationID: a.account.OrganizationID, AccountID: a.account.ID, AssetKind: sql.NullInt32{Int32: kind, Valid: true}, AssetID: reportingNullID(asset), Kinds: kinds, WindowStart: start, WindowEnd: reportingThrough(end), WindowEndSeconds: reportingThrough(end).Unix(), AfterSeconds: math.MinInt64, PageLimit: 200}
	for {
		rows, e := q.ListReportingAssetSupportMetadata(ctx, args)
		if e != nil {
			return e
		}
		if len(rows) == 0 {
			return nil
		}
		meta := make([]reportingFactMetadata, 0, len(rows))
		for _, r := range rows {
			meta = append(meta, reportingFactMetadata{r.RowID.Int64, reporting.FactKind(r.Kind), r.FactID, r.PayloadBytes.Int64})
		}
		meta = reportingFactPrefix(meta)
		facts, e := reportingFactPayloads(ctx, q, a.account.OrganizationID, a.account.ID, meta)
		if e != nil {
			return e
		}
		for _, f := range facts {
			if e = ctx.Err(); e != nil {
				return fmt.Errorf("visit reporting support: %w", e)
			}
			if e = visit(f); e != nil {
				return e
			}
		}
		last := rows[len(meta)-1]
		args.AfterSeconds, args.AfterNanos, args.AfterRowID = last.StartSeconds, last.StartNanos, last.RowID.Int64
	}
}

func validateReportingFinalFact(ctx context.Context, q sqlc.Querier, a reportingAccess, config reporting.Configuration, f reporting.Fact, references map[reporting.FactKey]any, changed bool) error {
	_, value, e := reporting.DecodeFact(f.Kind, f.Payload)
	if e != nil {
		return e
	}
	if e = reporting.ValidateFactFields(value); e != nil {
		return fleeterror.NewInvalidArgumentError(e.Error())
	}
	if f.OrderID != "" {
		if _, ok := references[reporting.FactKey{Kind: reporting.FactOrder, ID: f.OrderID}]; !ok {
			return fleeterror.NewInvalidArgumentErrorf("%s %q: order %q is unavailable", f.Kind, f.ID, f.OrderID)
		}
		order, ok := references[reporting.FactKey{Kind: reporting.FactOrder, ID: f.OrderID}].(reporting.Order)
		if !ok {
			return fmt.Errorf("stored hosting order has an unexpected payload type")
		}
		if f.Kind != reporting.FactExternalResult && (f.Start.Before(order.Start) || (!order.End.IsZero() && (f.End.IsZero() || f.End.After(order.End)))) {
			return fleeterror.NewInvalidArgumentError("attribution exceeds its hosting order")
		}
	}
	switch v := value.(type) {
	case reporting.CoverageSegment, reporting.ModuleCoverageSegment:
		if changed {
			kinds := []string{string(reporting.FactCoverage)}
			if f.AssetKind == reporting.AssetKindModule {
				kinds = append(kinds, string(reporting.FactModuleCoverage))
			}
			e = visitReportingAssetSupport(ctx, q, a, f.AssetKind, f.AssetID, f.Start, f.End, kinds, func(peer reporting.Fact) error {
				if peer.Kind == f.Kind && peer.ID == f.ID {
					return nil
				}
				if reportingOverlap(f.Start, f.End, peer.Start, peer.End) {
					return fleeterror.NewInvalidArgumentError("overlapping commercial or module coverage")
				}
				return nil
			})
			if e != nil {
				return e
			}
		}
		if m, ok := v.(reporting.ModuleCoverageSegment); ok {
			boards := map[int64]bool{}
			for _, id := range m.BoardCoverageIDs {
				if _, ok := references[reporting.FactKey{Kind: reporting.FactCoverage, ID: id}]; !ok {
					return fleeterror.NewInvalidArgumentErrorf("%s %q: coverage %q is unavailable", f.Kind, f.ID, id)
				}
				c, ok := references[reporting.FactKey{Kind: reporting.FactCoverage, ID: id}].(reporting.CoverageSegment)
				if !ok {
					return fmt.Errorf("stored board coverage has an unexpected payload type")
				}
				if boards[c.AssetID] || !c.SupportsModule(m) {
					return fleeterror.NewInvalidArgumentError("module coverage exceeds its distinct attributed board coverage")
				}
				boards[c.AssetID] = true
			}
		}
	case reporting.MeterInterval:
		if v.AllocationFraction != "" && !config.SharedAllocationApproved() {
			return fleeterror.NewInvalidArgumentError("shared meter requires approved allocation evidence")
		}
		if changed {
			return validateReportingMeterPeers(ctx, q, a, v)
		}
	}
	return nil
}

func validateReportingCoverageUnions(ctx context.Context, q sqlc.Querier, a reportingAccess, facts []reporting.Fact) error {
	type requirement struct {
		fact                    reporting.Fact
		end, cursor             time.Time
		modules, point, covered bool
	}
	type asset struct {
		kind int32
		id   int64
	}
	groups := map[asset][]*requirement{}
	order := []asset{}
	for _, f := range facts {
		if f.Kind != reporting.FactHashrate && f.Kind != reporting.FactObligation && f.Kind != reporting.FactPoolObservation {
			continue
		}
		r := &requirement{fact: f, end: f.End, cursor: f.Start}
		if f.Kind == reporting.FactObligation {
			_, value, e := reporting.DecodeFact(f.Kind, f.Payload)
			if e != nil {
				return e
			}
			v, ok := value.(reporting.ObligationInterval)
			if !ok {
				return fmt.Errorf("stored obligation has an unexpected payload type")
			}
			r.modules, r.end = true, v.AsOf
			if !v.End.IsZero() && v.End.Before(r.end) {
				r.end = v.End
			}
		} else if f.Kind == reporting.FactPoolObservation {
			r.point, r.end = true, f.Start
		}
		key := asset{f.AssetKind, f.AssetID}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], r)
	}
	for _, key := range order {
		group := groups[key]
		start, end := group[0].fact.Start, group[0].end
		modules := false
		for _, r := range group {
			if r.fact.Start.Before(start) {
				start = r.fact.Start
			}
			if r.end.After(end) {
				end = r.end
			}
			modules = modules || r.modules
		}
		kinds := []string{string(reporting.FactCoverage)}
		if modules {
			kinds = append(kinds, string(reporting.FactModuleCoverage))
		}
		e := visitReportingAssetSupport(ctx, q, a, key.kind, key.id, start, end, kinds, func(c reporting.Fact) error {
			for _, r := range group {
				f := r.fact
				if r.covered || (!r.modules && c.Kind != reporting.FactCoverage) {
					continue
				}
				if r.point {
					if !f.Start.Before(c.Start) && (c.End.IsZero() || f.Start.Before(c.End)) {
						r.covered = true
					}
					continue
				}
				if c.SiteID != f.SiteID || c.ContainerID != f.ContainerID || c.OrderID != f.OrderID || c.Start.After(r.cursor) || (!c.End.IsZero() && !c.End.After(r.cursor)) {
					continue
				}
				if c.End.IsZero() || !c.End.Before(r.end) {
					r.covered = true
				} else {
					r.cursor = c.End
				}
			}
			return nil
		})
		if e != nil {
			return e
		}
		for _, r := range group {
			if !r.covered {
				return fleeterror.NewInvalidArgumentErrorf("%s %q: interval or source point exceeds its attributed commercial coverage", r.fact.Kind, r.fact.ID)
			}
		}
	}
	return nil
}

func validateReportingMeterPeers(ctx context.Context, q sqlc.Querier, a reportingAccess, m reporting.MeterInterval) error {
	args := sqlc.ListReportingMeterSupportMetadataParams{OrganizationID: a.account.OrganizationID, AccountID: a.account.ID, MeterID: reportingNullText(m.MeterID), WindowStart: m.Start, WindowEnd: m.End, PageLimit: 200}
	total := new(big.Rat)
	for {
		rows, e := q.ListReportingMeterSupportMetadata(ctx, args)
		if e != nil {
			return e
		}
		if len(rows) == 0 {
			break
		}
		meta := []reportingFactMetadata{}
		for _, r := range rows {
			meta = append(meta, reportingFactMetadata{r.RowID.Int64, reporting.FactKind(r.Kind), r.FactID, r.PayloadBytes.Int64})
		}
		meta = reportingFactPrefix(meta)
		facts, e := reportingFactPayloads(ctx, q, a.account.OrganizationID, a.account.ID, meta)
		if e != nil {
			return e
		}
		for _, f := range facts {
			_, value, e := reporting.DecodeFact(f.Kind, f.Payload)
			if e != nil {
				return e
			}
			p, ok := value.(reporting.MeterInterval)
			if !ok {
				return fmt.Errorf("stored meter has an unexpected payload type")
			}
			if !reportingOverlap(m.Start, m.End, p.Start, p.End) {
				continue
			}
			if p.ID != m.ID && (p.OrderID == m.OrderID || !p.SharesEvidenceWith(m)) {
				return fleeterror.NewInvalidArgumentError("overlapping meter intervals require matching evidence and distinct reviewed allocation shares")
			}
			fraction := big.NewRat(1, 1)
			if p.AllocationFraction != "" {
				fraction, e = reporting.Decimal(p.AllocationFraction, false)
				if e != nil {
					return e
				}
			}
			total.Add(total, fraction)
			if total.Cmp(big.NewRat(1, 1)) > 0 {
				return fleeterror.NewInvalidArgumentError("shared meter allocation exceeds one")
			}
		}
		args.AfterRowID = meta[len(meta)-1].rowID
	}
	return nil
}

// validateReportingExternalWitnesses rechecks mutable physical evidence for the
// complete final state. Ordinary meter/fee/site claims use metadata only. Payloads
// are read only for potential exact allocation-boundary violations and mappings.
// The caller must hold the header and all sorted device fences before this call.
func validateReportingExternalWitnesses(ctx context.Context, q sqlc.Querier, a reportingAccess) error {
	args := sqlc.ListReportingWitnessMetadataParams{OrganizationID: a.account.OrganizationID, AccountID: a.account.ID, PageLimit: 200}
	for {
		rows, e := q.ListReportingWitnessMetadata(ctx, args)
		if e != nil {
			return e
		}
		if len(rows) == 0 {
			return nil
		}
		meta := []reportingFactMetadata{}
		for _, r := range rows {
			if r.NeedsHistory {
				meta = append(meta, reportingFactMetadata{r.RowID.Int64, reporting.FactKind(r.Kind), r.FactID, r.PayloadBytes.Int64})
			}
		}
		selected := reportingFactPrefix(meta)
		last := rows[len(rows)-1].RowID.Int64
		if len(selected) < len(meta) {
			last = selected[len(selected)-1].rowID
		}
		input := reporting.Inputs{}
		for _, r := range rows {
			if r.RowID.Int64 > last {
				break
			}
			input.ExternalResults = append(input.ExternalResults, reporting.ContractResult{SiteID: r.SiteID.Int64, ContainerID: r.ContainerID.Int64})
		}
		facts, e := reportingFactPayloads(ctx, q, a.account.OrganizationID, a.account.ID, selected)
		if e != nil {
			return e
		}
		keys := []reporting.FactKey{}
		for _, f := range facts {
			_, v, e := reporting.DecodeFact(f.Kind, f.Payload)
			if e != nil {
				return e
			}
			switch v := v.(type) {
			case reporting.CoverageSegment:
				input.Coverage = append(input.Coverage, v)
			case reporting.ModuleCoverageSegment:
				input.ModuleCoverage = append(input.ModuleCoverage, v)
				for _, id := range v.BoardCoverageIDs {
					keys = append(keys, reporting.FactKey{Kind: reporting.FactCoverage, ID: id})
				}
			}
		}
		references, e := reportingFactReferences(ctx, q, a, keys)
		if e != nil {
			return e
		}
		for _, module := range input.ModuleCoverage {
			for _, id := range module.BoardCoverageIDs {
				if _, ok := references[reporting.FactKey{Kind: reporting.FactCoverage, ID: id}]; !ok {
					return fleeterror.NewInvalidArgumentErrorf("module_coverage %q: coverage %q is unavailable", module.ID, id)
				}
			}
		}
		for _, value := range references {
			board, ok := value.(reporting.CoverageSegment)
			if !ok {
				return fmt.Errorf("stored board coverage has an unexpected payload type")
			}
			input.Coverage = append(input.Coverage, board)
		}
		if e = validateReportingExternalFacts(ctx, q, a, input); e != nil {
			return e
		}
		args.AfterRowID = last
	}
}
