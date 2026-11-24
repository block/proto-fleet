package hostedaccounts_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
)

type reportingInputsFixtureRecord struct {
	hostedaccounts.ReportingInputsMetadata
	Inputs reporting.Inputs
}

func reportingFixtureFacts(input reporting.Inputs) ([]reporting.Fact, error) {
	values := []any{}
	for _, v := range input.Orders {
		values = append(values, v)
	}
	for _, v := range input.Coverage {
		values = append(values, v)
	}
	for _, v := range input.ModuleCoverage {
		values = append(values, v)
	}
	for _, v := range input.Meters {
		values = append(values, v)
	}
	for _, v := range input.Fees {
		values = append(values, v)
	}
	for _, v := range input.Incidents {
		values = append(values, v)
	}
	for _, v := range input.Hashrates {
		values = append(values, v)
	}
	for _, v := range input.PoolObservations {
		values = append(values, v)
	}
	for _, v := range input.Obligations {
		values = append(values, v)
	}
	for _, v := range input.ExternalResults {
		values = append(values, v)
	}
	facts := make([]reporting.Fact, 0, len(values))
	for _, v := range values {
		f, e := reporting.EncodeFact(v)
		if e != nil {
			return nil, e
		}
		facts = append(facts, f)
	}
	return facts, nil
}

// saveReportingInputsForTest adapts existing small complete-state fixtures to real
// bounded operations. Larger setup fixtures explicitly span multiple revisions;
// this helper does not promise whole-file atomicity. Production has no replace API.
func saveReportingInputsForTest(s *hostedaccounts.Service, ctx context.Context, actor, org int64, account string, expected int64, source string, input reporting.Inputs) (reportingInputsFixtureRecord, error) {
	var out reportingInputsFixtureRecord
	facts, e := reportingFixtureFacts(input)
	if e != nil {
		_, e = s.MutateReportingInputs(ctx, actor, org, account, expected, source, &input.Configuration, []reporting.FactMutation{{Kind: reporting.FactOrder, ID: "invalid", Payload: json.RawMessage(`{}`)}})
		return out, e
	}
	prior := map[reporting.FactKey]reporting.Fact{}
	metadata, e := s.ReportingInputsMetadata(ctx, actor, org, account)
	if e == nil && metadata.Revision == expected {
		for _, kind := range reportingFixtureKinds {
			cursor := ""
			for {
				page, e := s.ReportingFactPage(ctx, actor, org, account, kind, expected, cursor, 200)
				if e != nil {
					return out, e
				}
				for _, f := range page.Facts {
					prior[reporting.FactKey{Kind: f.Kind, ID: f.ID}] = f
				}
				if page.NextCursor == "" {
					break
				}
				cursor = page.NextCursor
			}
		}
	}
	changes := []reporting.FactMutation{}
	seen := map[reporting.FactKey]bool{}
	for _, f := range facts {
		key := reporting.FactKey{Kind: f.Kind, ID: f.ID}
		if seen[key] {
			return out, fmt.Errorf("duplicate fixture reporting fact")
		}
		seen[key] = true
		old, exists := prior[key]
		if !exists || string(old.Payload) != string(f.Payload) {
			changes = append(changes, reporting.FactMutation{Kind: f.Kind, ID: f.ID, Payload: f.Payload})
		}
		delete(prior, key)
	}
	deletes := []reporting.FactMutation{}
	for key := range prior {
		deletes = append(deletes, reporting.FactMutation{Kind: key.Kind, ID: key.ID, Delete: true})
	}
	sort.Slice(deletes, func(i, j int) bool {
		if deletes[i].Kind != deletes[j].Kind {
			return deletes[i].Kind < deletes[j].Kind
		}
		return deletes[i].ID < deletes[j].ID
	})
	changes = append(changes, deletes...)
	revision := expected
	for first := true; first || len(changes) > 0; first = false {
		n := min(len(changes), reporting.MaxFactMutationsPerRequest)
		for n > 1 {
			b, err := json.Marshal(changes[:n])
			if err != nil {
				return out, fmt.Errorf("encode fixture changes: %w", err)
			}
			if len(b) <= reporting.MaxFactMutationBytes {
				break
			}
			n--
		}
		m, e := s.MutateReportingInputs(ctx, actor, org, account, revision, source, &input.Configuration, changes[:n])
		if e != nil {
			return out, e
		}
		out.ReportingInputsMetadata = m
		revision = m.Revision
		changes = changes[n:]
	}
	out.Inputs = input
	return out, nil
}

var reportingFixtureKinds = []reporting.FactKind{reporting.FactOrder, reporting.FactCoverage, reporting.FactModuleCoverage, reporting.FactMeter, reporting.FactFee, reporting.FactIncident, reporting.FactHashrate, reporting.FactPoolObservation, reporting.FactObligation, reporting.FactExternalResult}

// reportingInputsForTest materializes only existing small assertion fixtures via
// the admitted public pages. Scale tests count pages without using this helper.
func reportingInputsForTest(s *hostedaccounts.Service, ctx context.Context, actor, org int64, account string) (reportingInputsFixtureRecord, error) {
	var out reportingInputsFixtureRecord
	m, e := s.ReportingInputsMetadata(ctx, actor, org, account)
	if e != nil {
		return out, e
	}
	out.ReportingInputsMetadata = m
	out.Inputs.Configuration = m.Configuration
	for _, kind := range reportingFixtureKinds {
		cursor := ""
		for {
			page, e := s.ReportingFactPage(ctx, actor, org, account, kind, m.Revision, cursor, 200)
			if e != nil {
				return out, e
			}
			for _, f := range page.Facts {
				_, v, e := reporting.DecodeFact(kind, f.Payload)
				if e != nil {
					return out, e
				}
				switch v := v.(type) {
				case reporting.Order:
					out.Inputs.Orders = append(out.Inputs.Orders, v)
				case reporting.CoverageSegment:
					out.Inputs.Coverage = append(out.Inputs.Coverage, v)
				case reporting.ModuleCoverageSegment:
					out.Inputs.ModuleCoverage = append(out.Inputs.ModuleCoverage, v)
				case reporting.MeterInterval:
					out.Inputs.Meters = append(out.Inputs.Meters, v)
				case reporting.FeeLine:
					out.Inputs.Fees = append(out.Inputs.Fees, v)
				case reporting.Incident:
					out.Inputs.Incidents = append(out.Inputs.Incidents, v)
				case reporting.HashrateInterval:
					out.Inputs.Hashrates = append(out.Inputs.Hashrates, v)
				case reporting.PoolObservation:
					out.Inputs.PoolObservations = append(out.Inputs.PoolObservations, v)
				case reporting.ObligationInterval:
					out.Inputs.Obligations = append(out.Inputs.Obligations, v)
				case reporting.ContractResult:
					out.Inputs.ExternalResults = append(out.Inputs.ExternalResults, v)
				}
			}
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
	}
	return out, nil
}
