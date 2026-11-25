package reporting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// SourceInputsStream yields only the selected original facts, in stable identity
// order within each register. The writer never buffers a register or artifact.
type SourceInputsStream func(FactKind, func(json.RawMessage) error) error

const MaxSourceInputsBytes = 64 << 20

func WriteSourceInputsStream(ctx context.Context, destination io.Writer, p Period, configuration Configuration, stream SourceInputsStream) error {
	w := &outputWriter{checkCtx: ctx.Err, writer: destination, remaining: MaxSourceInputsBytes, bounded: true}
	write := func(text string) error {
		_, err := io.WriteString(w, text)
		if err != nil {
			return fmt.Errorf("write report source inputs: %w", err)
		}
		return nil
	}
	encode := func(value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode report source field: %w", err)
		}
		_, err = w.Write(data)
		return err
	}
	if err := write(`{"schema":"hosted-report-source-inputs/v1","period":`); err != nil {
		return err
	}
	if err := encode(p); err != nil {
		return err
	}
	if err := write(`,"inputs":{"configuration":`); err != nil {
		return err
	}
	if err := encode(configuration); err != nil {
		return err
	}
	for _, register := range []struct {
		kind FactKind
		name string
	}{
		{FactModuleCoverage, "moduleCoverage"}, {FactOrder, "orders"}, {FactCoverage, "coverage"}, {FactMeter, "meters"}, {FactFee, "fees"},
		{FactIncident, "incidents"}, {FactHashrate, "hashrates"}, {FactPoolObservation, "poolObservations"}, {FactObligation, "obligations"}, {FactExternalResult, "externalResults"},
	} {
		if err := write(`,"` + register.name + `":[`); err != nil {
			return err
		}
		first := true
		err := stream(register.kind, func(payload json.RawMessage) error {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("stream report source inputs: %w", err)
			}
			if !first {
				if err := write(","); err != nil {
					return err
				}
			}
			first = false
			_, err := w.Write(payload)
			return err
		})
		if err != nil {
			return fmt.Errorf("stream report source inputs: %w", err)
		}
		if err := write("]"); err != nil {
			return err
		}
	}
	return write("}}\n")
}
