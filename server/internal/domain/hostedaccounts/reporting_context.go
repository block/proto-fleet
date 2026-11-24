package hostedaccounts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
)

const selectedReportInputBytes int64 = 32 << 20
const selectedReportInputHeapBytes int64 = 256 << 20
const selectedReportContextTimeout = 45 * time.Second

type reportingContext struct {
	Revision int64
	Inputs   reporting.Inputs
	metadata []reportingFactMetadata
}

func latestReportInput(ctx context.Context, q sqlc.Querier, a reportingAccess, revision int64, period reporting.Period) (reportingContext, error) {
	ctx, cancel := context.WithTimeout(ctx, selectedReportContextTimeout)
	defer cancel()
	var zero reportingContext
	row, err := q.LockReportingInputsForShare(ctx, sqlc.LockReportingInputsForShareParams{OrganizationID: a.account.OrganizationID, AccountID: a.account.ID})
	if err != nil {
		return zero, accessError(err)
	}
	if !a.sitesAllowed(row.SiteIds) {
		return zero, denied()
	}
	if row.Revision != revision {
		return zero, reportingConflict()
	}
	if err = lockReportingFactDevices(ctx, q, a, nil); err != nil {
		return zero, err
	}
	if err = validateReportingExternalWitnesses(ctx, q, a); err != nil {
		return zero, err
	}
	var config reporting.Configuration
	if err = json.Unmarshal(row.Configuration, &config); err != nil {
		return zero, fmt.Errorf("decode reporting configuration: %w", err)
	}
	out, err := loadReportingContext(ctx, q, row.OrganizationID, row.AccountID, config, period)
	out.Revision = row.Revision
	return out, err
}

// loadReportingContext requires its caller to hold the shared input header lock.
// Admission is the caller's responsibility; this helper performs no actor checks.
func loadReportingContext(ctx context.Context, q sqlc.Querier, org, account int64, config reporting.Configuration, p reporting.Period) (reportingContext, error) {
	var out reportingContext
	out.Inputs.Configuration = config
	start := p.Start
	if p.Kind == "annual" {
		loc, err := time.LoadLocation(p.Timezone)
		if err != nil {
			return out, fmt.Errorf("report context timezone: %w", err)
		}
		start = p.Start.In(loc).AddDate(-1, 0, 0)
	}
	size, err := q.ReportingContextSize(ctx, sqlc.ReportingContextSizeParams{OrganizationID: org, AccountID: account, WindowStart: start, WindowSeconds: start.Unix(), WindowNanos: int64(start.Nanosecond()), PeriodEnd: p.End, AsOf: p.AsOf, EndSeconds: p.End.Unix(), EndNanos: int64(p.End.Nanosecond())})
	if err != nil {
		return out, err
	}
	if size.PayloadBytes > selectedReportInputBytes || size.FactCount > (selectedReportInputHeapBytes-8*reporting.MaxReportingConfigurationBytes-8*size.PayloadBytes-6*size.IdentityBytes)/2048 {
		return out, fleeterror.NewFailedPreconditionError("selected report input exceeds byte or heap budget")
	}
	args := sqlc.ListReportingContextMetadataParams{OrganizationID: org, AccountID: account, WindowStart: start, WindowSeconds: start.Unix(), WindowNanos: int64(start.Nanosecond()), PeriodEnd: p.End, AsOf: p.AsOf, PageLimit: max(1, size.FactCount), EndSeconds: p.End.Unix(), EndNanos: int64(p.End.Nanosecond())}
	out.metadata = make([]reportingFactMetadata, 0, int(size.FactCount))
	if err := ctx.Err(); err != nil {
		return out, fmt.Errorf("select report context: %w", err)
	}
	rows, err := q.ListReportingContextMetadata(ctx, args)
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		out.metadata = append(out.metadata, reportingFactMetadata{r.RowID.Int64, reporting.FactKind(r.Kind), r.FactID, r.PayloadBytes.Int64})
	}
	if int64(len(out.metadata)) != size.FactCount {
		return out, fmt.Errorf("report context changed under revision lock")
	}
	err = streamReportingContextFacts(ctx, q, org, account, out.metadata, func(f reporting.Fact) error {
		_, v, err := reporting.DecodeFact(f.Kind, f.Payload)
		if err != nil {
			return err
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
		return nil
	})
	return out, err
}

func streamReportingContextFacts(ctx context.Context, q sqlc.Querier, org, account int64, meta []reportingFactMetadata, yield func(reporting.Fact) error) error {
	for len(meta) > 0 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("stream report context: %w", err)
		}
		prefix := reportingFactPrefix(meta[:min(200, len(meta))])
		facts, err := reportingFactPayloads(ctx, q, org, account, prefix)
		if err != nil {
			return err
		}
		for _, f := range facts {
			if err := yield(f); err != nil {
				return err
			}
		}
		meta = meta[len(prefix):]
	}
	return nil
}

func writeReportingContextSource(ctx context.Context, w io.Writer, q sqlc.Querier, org, account int64, p reporting.Period, in reportingContext) error {
	return reporting.WriteSourceInputsStream(ctx, w, p, in.Inputs.Configuration, func(kind reporting.FactKind, yield func(json.RawMessage) error) error {
		first := 0
		for first < len(in.metadata) && in.metadata[first].kind < kind {
			first++
		}
		last := first
		for last < len(in.metadata) && in.metadata[last].kind == kind {
			last++
		}
		return streamReportingContextFacts(ctx, q, org, account, in.metadata[first:last], func(f reporting.Fact) error { return yield(f.Payload) })
	})
}
