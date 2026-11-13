package reporting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// WriteSourceInputs freezes the host's source values once at publication. Keep
// original intervals (including crossings), prior-year comparison evidence and
// earlier episode/installation context; these are evidence, not prorated values.
func writeFixtureSourceInputs(ctx context.Context, w io.Writer, p Period, in Inputs) error {
	selected, err := sourceInputs(ctx, p, in)
	if err != nil {
		return err
	}
	return WriteSourceInputsStream(ctx, w, p, selected.Configuration, func(kind FactKind, yield func(json.RawMessage) error) error {
		emit := func(value any) error {
			data, err := json.Marshal(value)
			if err != nil {
				return fmt.Errorf("encode fixture source: %w", err)
			}
			return yield(data)
		}
		switch kind {
		case FactOrder:
			for _, v := range selected.Orders {
				if err := emit(v); err != nil {
					return err
				}
			}
		case FactCoverage:
			for _, v := range selected.Coverage {
				if err := emit(v); err != nil {
					return err
				}
			}
		case FactModuleCoverage:
			for _, v := range selected.ModuleCoverage {
				if err := emit(v); err != nil {
					return err
				}
			}
		case FactMeter:
			for _, v := range selected.Meters {
				if err := emit(v); err != nil {
					return err
				}
			}
		case FactFee:
			for _, v := range selected.Fees {
				if err := emit(v); err != nil {
					return err
				}
			}
		case FactIncident:
			for _, v := range selected.Incidents {
				if err := emit(v); err != nil {
					return err
				}
			}
		case FactHashrate:
			for _, v := range selected.Hashrates {
				if err := emit(v); err != nil {
					return err
				}
			}
		case FactPoolObservation:
			for _, v := range selected.PoolObservations {
				if err := emit(v); err != nil {
					return err
				}
			}
		case FactObligation:
			for _, v := range selected.Obligations {
				if err := emit(v); err != nil {
					return err
				}
			}
		case FactExternalResult:
			for _, v := range selected.ExternalResults {
				if err := emit(v); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func sourceInputs(ctx context.Context, p Period, in Inputs) (Inputs, error) {
	start := p.Start
	if p.Kind == "annual" {
		start = p.Start.In(mustLocation(p.Timezone)).AddDate(-1, 0, 0)
	}
	relevant := func(a, b time.Time) bool { return overlaps(start, p.End, a, b) }
	out := Inputs{Configuration: in.Configuration}
	modules := map[int64]bool{}
	for _, v := range in.Obligations {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select report source inputs: %w", err)
		}
		// Earlier segments establish true episode starts and current open-ticket state.
		if v.Start.Before(p.End) && !v.ReceivedAt.After(p.AsOf) {
			out.Obligations = append(out.Obligations, v)
			modules[v.AssetID] = true
		}
	}
	boards := map[string]bool{}
	for _, v := range in.ModuleCoverage {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select report source inputs: %w", err)
		}
		if relevant(v.Start, v.End) || modules[v.ModuleID] {
			out.ModuleCoverage = append(out.ModuleCoverage, v)
			for _, id := range v.BoardCoverageIDs {
				boards[id] = true
			}
		}
	}
	for _, v := range in.Coverage {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select report source inputs: %w", err)
		}
		if relevant(v.Start, v.End) || boards[v.ID] || (v.Kind == 6 && modules[v.AssetID]) {
			out.Coverage = append(out.Coverage, v)
		}
	}
	for _, v := range in.Meters {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select report source inputs: %w", err)
		}
		if relevant(v.Start, v.End) {
			out.Meters = append(out.Meters, v)
		}
	}
	for _, v := range in.Fees {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select report source inputs: %w", err)
		}
		if relevant(v.Start, v.End) {
			out.Fees = append(out.Fees, v)
		}
	}
	for _, v := range in.Hashrates {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select report source inputs: %w", err)
		}
		if relevant(v.Start, v.End) {
			out.Hashrates = append(out.Hashrates, v)
		}
	}
	for _, v := range in.Incidents {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select report source inputs: %w", err)
		}
		if relevant(v.Start, v.End) {
			out.Incidents = append(out.Incidents, v)
		}
	}
	for _, v := range in.ExternalResults {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select report source inputs: %w", err)
		}
		if relevant(v.Start, v.End) {
			out.ExternalResults = append(out.ExternalResults, v)
		}
	}
	for _, v := range in.PoolObservations {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select report source inputs: %w", err)
		}
		if v.ReceivedAt.After(p.AsOf) || !v.SourceAt.Before(p.End) {
			continue
		}
		for _, c := range out.Coverage {
			if c.Kind == v.Kind && c.AssetID == v.AssetID && !v.SourceAt.Before(c.Start) && (c.End.IsZero() || v.SourceAt.Before(c.End)) {
				out.PoolObservations = append(out.PoolObservations, v)
				break
			}
		}
	}
	orders := map[string]bool{}
	for _, v := range out.Coverage {
		orders[v.OrderID] = true
	}
	for _, v := range out.ModuleCoverage {
		orders[v.OrderID] = true
	}
	for _, v := range out.Meters {
		orders[v.OrderID] = true
	}
	for _, v := range out.Fees {
		orders[v.OrderID] = true
	}
	for _, v := range out.Hashrates {
		orders[v.OrderID] = true
	}
	for _, v := range out.Incidents {
		orders[v.OrderID] = true
	}
	for _, v := range out.Obligations {
		orders[v.OrderID] = true
	}
	for _, v := range out.ExternalResults {
		orders[v.OrderID] = true
	}
	for _, v := range in.Orders {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select report source inputs: %w", err)
		}
		if orders[v.ID] || relevant(v.Start, v.End) {
			out.Orders = append(out.Orders, v)
		}
	}
	if err := ctx.Err(); err != nil {
		return Inputs{}, fmt.Errorf("select report source inputs: %w", err)
	}
	return out, nil
}
