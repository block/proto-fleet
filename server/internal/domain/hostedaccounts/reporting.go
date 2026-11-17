package hostedaccounts

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const reportingTransactionTimeout = 60 * time.Second
const reportDeliveryConfirmed = "confirmed"
const reportingClockSkew = 5 * time.Second

// The cutoff waits for at most one run of the hourly evidence job.
const reportEvidenceCutoffTimeout = 15 * time.Second

type ReportRecord struct {
	ID               int64              `json:"-"`
	ReportID         string             `json:"reportId"`
	SeriesID         string             `json:"seriesId"`
	AccountID        string             `json:"accountId"`
	AccountName      string             `json:"accountName"`
	Number           int64              `json:"number"`
	Revision         int64              `json:"revision"`
	InputRevision    int64              `json:"inputRevision"`
	Period           reporting.Period   `json:"period"`
	Result           reporting.Result   `json:"result"`
	Status           string             `json:"status"`
	CreatedBy        int64              `json:"createdBy"`
	OwnerID          int64              `json:"ownerId"`
	IssuedAt         *time.Time         `json:"issuedAt,omitempty"`
	IssuedBy         int64              `json:"issuedBy,omitempty"`
	CorrectionOf     string             `json:"correctionOf,omitempty"`
	CorrectionReason string             `json:"correctionReason,omitempty"`
	Exception        string             `json:"exception,omitempty"`
	SiteIDs          []int64            `json:"siteIds"`
	Deliveries       []DeliveryEvidence `json:"deliveries"`
	DeliveryStatus   string             `json:"deliveryStatus,omitempty"`
}
type DraftReportOptions struct {
	Period           reporting.Period
	InputRevision    int64
	ReportID         string
	ExpectedRevision int64
	CorrectionOf     string
	CorrectionReason string
	OwnerUserID      string
}
type DeliveryEvidence struct {
	ID              int64      `json:"id,omitempty"`
	RequestKey      string     `json:"requestKey"`
	Channel         string     `json:"channel"`
	Status          string     `json:"status"`
	SentAt          *time.Time `json:"sentAt,omitempty"`
	ReceivedAt      *time.Time `json:"receivedAt,omitempty"`
	SourceRef       string     `json:"sourceRef"`
	Evidence        string     `json:"evidence"`
	FailureCategory string     `json:"failureCategory,omitempty"`
	ActorID         int64      `json:"actorId,omitempty"`
	CreatedAt       time.Time  `json:"createdAt,omitempty"`
}

func reportTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}
func decodeReport(row sqlc.HostedReport) (ReportRecord, error) {
	if len(row.Result) > reporting.MaxResultJSONBytes {
		return ReportRecord{}, fleeterror.NewFailedPreconditionError("report result exceeds supported byte limit")
	}
	out := ReportRecord{ID: row.ID, ReportID: row.ReportID, SeriesID: row.SeriesID, Number: row.Number, Revision: row.Revision, InputRevision: row.InputRevision, Status: row.Status, CreatedBy: row.CreatedBy, OwnerID: row.OwnerID, IssuedAt: reportTime(row.IssuedAt), IssuedBy: row.IssuedBy.Int64, CorrectionReason: row.CorrectionReason, Exception: row.IncompleteException, SiteIDs: row.SiteIds}
	if err := json.Unmarshal(row.Period, &out.Period); err != nil {
		return out, fmt.Errorf("decode report period: %w", err)
	}
	if err := json.Unmarshal(row.Result, &out.Result); err != nil {
		return out, fmt.Errorf("decode report result: %w", err)
	}
	return out, nil
}
func decodeDelivery(row sqlc.HostedReportDelivery) DeliveryEvidence {
	return DeliveryEvidence{ID: row.ID, RequestKey: row.RequestKey, Channel: row.Channel, Status: row.Status, SentAt: reportTime(row.SentAt), ReceivedAt: reportTime(row.ReceivedAt), SourceRef: row.SourceRef, Evidence: row.Evidence, FailureCategory: row.FailureCategory, ActorID: row.ActorID, CreatedAt: row.CreatedAt}
}

// reportDeliveryHistoryLimit bounds the delivery attempts a host read returns,
// newest kept; the status still reflects every attempt.
const reportDeliveryHistoryLimit = 100

func loadReportRecord(ctx context.Context, q sqlc.Querier, account Account, row sqlc.HostedReport, host bool) (ReportRecord, error) {
	out, err := decodeReport(row)
	if err != nil {
		return out, err
	}
	out.AccountID = account.AccountID
	out.AccountName = account.Name
	if row.CorrectionOf.Valid {
		out.CorrectionOf, err = q.HostedReportPublicID(ctx, row.CorrectionOf.Int64)
		if err != nil {
			return out, err
		}
	}
	out.DeliveryStatus, err = q.HostedReportDeliveryStatus(ctx, row.ID)
	if err != nil || !host {
		return out, err
	}
	deliveries, err := q.ListHostedReportDeliveries(ctx, sqlc.ListHostedReportDeliveriesParams{ReportInternalID: row.ID, PageLimit: reportDeliveryHistoryLimit})
	if err != nil {
		return out, err
	}
	slices.Reverse(deliveries)
	out.Deliveries = make([]DeliveryEvidence, 0, len(deliveries))
	for _, d := range deliveries {
		out.Deliveries = append(out.Deliveries, decodeDelivery(d))
	}
	return out, nil
}
func (s *Service) PrepareReport(ctx context.Context, actorID, orgID int64, accountID string, opts DraftReportOptions) (ReportRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, reportingTransactionTimeout)
	defer cancel()
	var zero ReportRecord
	opts.Period.Start = opts.Period.Start.Round(time.Microsecond)
	opts.Period.End = opts.Period.End.Round(time.Microsecond)
	opts.Period.AsOf = opts.Period.AsOf.UTC().Round(time.Microsecond)
	opts.Period.RequestReceivedAt = opts.Period.RequestReceivedAt.Round(time.Microsecond)
	if opts.Period.AsOf.After(reportingEvidenceTimeLimit()) {
		return zero, fleeterror.NewInvalidArgumentError("report as-of cannot be in the future")
	}
	if err := opts.Period.Validate(); err != nil {
		return zero, fleeterror.NewInvalidArgumentError(err.Error())
	}
	if opts.InputRevision <= 0 || opts.ExpectedRevision < 0 || opts.OwnerUserID == "" || (opts.ReportID != "" && (opts.ExpectedRevision == 0 || opts.CorrectionOf != "")) || (opts.ReportID == "" && opts.ExpectedRevision != 0) || (opts.CorrectionOf == "") != (strings.TrimSpace(opts.CorrectionReason) == "") || len(opts.CorrectionReason) > 2000 {
		return zero, fleeterror.NewInvalidArgumentError("invalid report identity, correction, owner or revision")
	}
	// Admit the actor before taking the cutoff, so a caller without reporting
	// authority never share-locks the control row that every account's evidence
	// writers use. Preparation admits again under its own locks.
	if _, err := db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (reportingAccess, error) {
		return reportingHostReadAdmission(ctx, q, actorID, orgID, accountID, "prepare")
	}); err != nil {
		return zero, reportExecutionError(ctx, err)
	}
	// Freeze monitoring evidence by time, in a transaction of its own: the cutoff
	// briefly share-locks the evidence writers' control row, and the preparation
	// below must not hold that lock. A regenerated draft takes a new cutoff.
	cutoff, err := db.WithTransactionTimeout(ctx, s.conn, reportEvidenceCutoffTimeout, func(q sqlc.Querier) (time.Time, error) {
		return q.SelectHostedReportEvidenceCutoff(ctx, opts.Period.AsOf)
	})
	if err != nil {
		return zero, reportEvidenceCutoffError(ctx, err)
	}
	evidenceCutoff := sql.NullTime{Time: cutoff, Valid: true}
	record, err := db.WithTransactionTimeout(ctx, s.conn, reportingTransactionTimeout, func(q sqlc.Querier) (ReportRecord, error) {
		a, err := reportingHostReadAdmission(ctx, q, actorID, orgID, accountID, "prepare")
		if err != nil {
			return zero, err
		}
		// Serialize originals, corrections and input edits for this account while
		// shared organization admission lets hosted-account readers continue.
		header, err := q.LockReportingInputsForReport(ctx, sqlc.LockReportingInputsForReportParams{OrganizationID: orgID, AccountID: a.account.ID})
		if err != nil {
			return zero, accessError(err)
		}
		if !a.sitesAllowed(header.SiteIds) {
			return zero, denied()
		}
		if header.Revision != opts.InputRevision {
			return zero, reportingConflict()
		}
		var correctionParent *sqlc.HostedReport
		if opts.CorrectionOf != "" {
			original, err := q.GetHostedReport(ctx, sqlc.GetHostedReportParams{OrganizationID: orgID, AccountID: a.account.ID, ReportID: opts.CorrectionOf})
			if err != nil {
				return zero, accessError(err)
			}
			if original.Status != "issued" {
				return zero, reportingConflict()
			}
			if !a.sitesAllowed(original.SiteIds) {
				return zero, denied()
			}
			latest, err := q.LatestIssuedHostedReport(ctx, sqlc.LatestIssuedHostedReportParams{OrganizationID: orgID, AccountID: a.account.ID, SeriesID: original.SeriesID})
			if err != nil {
				return zero, err
			}
			if latest.ID != original.ID {
				return zero, fleeterror.NewFailedPreconditionError("open a correction of the latest issued report")
			}
			if !retainsCorrectionPeriod(original, opts.Period) {
				return zero, fleeterror.NewInvalidArgumentError("correction must retain the original report period")
			}
			existing, err := q.ActiveHostedReportCorrection(ctx, sqlc.ActiveHostedReportCorrectionParams{OrganizationID: orgID, AccountID: a.account.ID, CorrectionOf: sql.NullInt64{Int64: original.ID, Valid: true}})
			if err == nil {
				if !a.sitesAllowed(existing.SiteIds) {
					return zero, denied()
				}
				return loadReportRecord(ctx, q, a.account, existing, true)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return zero, err
			}
			correctionParent = &original
		}
		owner, err := q.GetReportingUser(ctx, sqlc.GetReportingUserParams{UserID: opts.OwnerUserID, OrganizationID: orgID})
		if err != nil {
			return zero, accessError(err)
		}
		if opts.Period.Kind == "ad_hoc" {
			if !a.accountWideRequestsAllowed() {
				return zero, denied()
			}
			if _, err := uuid.Parse(opts.Period.RequestRef); err != nil {
				return zero, fleeterror.NewInvalidArgumentError("ad hoc reports require a recorded request")
			}
			request, err := q.GetHostedReportRequest(ctx, sqlc.GetHostedReportRequestParams{OrganizationID: orgID, AccountID: a.account.ID, RequestID: opts.Period.RequestRef})
			if err != nil {
				return zero, accessError(err)
			}
			if !request.ReceivedAt.Equal(opts.Period.RequestReceivedAt) {
				return zero, fleeterror.NewInvalidArgumentError("ad hoc report receipt must match its recorded request")
			}
		}
		periodJSON, _ := json.Marshal(opts.Period)
		resultJSON, sites := []byte("{}"), []int64{}
		checkOriginalPeriod := func() error {
			if !slices.Contains([]string{"monthly", "quarterly", "annual"}, opts.Period.Kind) {
				return nil
			}
			existing, err := q.FindHostedOriginalReport(ctx, sqlc.FindHostedOriginalReportParams{OrganizationID: orgID, AccountID: a.account.ID, Kind: opts.Period.Kind, PeriodStart: opts.Period.Start, PeriodEnd: opts.Period.End, ReportID: opts.ReportID})
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			return fleeterror.NewFailedPreconditionError("an original report already exists for this period; continue report " + existing + " or prepare its correction")
		}
		var row sqlc.HostedReport
		if opts.ReportID != "" {
			previous, err := q.LockHostedReport(ctx, sqlc.LockHostedReportParams{OrganizationID: orgID, AccountID: a.account.ID, ReportID: opts.ReportID})
			if err != nil {
				return zero, accessError(err)
			}
			if !a.sitesAllowed(previous.SiteIds) {
				return zero, denied()
			}
			if previous.Kind == "ad_hoc" {
				var prior reporting.Period
				if err := json.Unmarshal(previous.Period, &prior); err != nil {
					return zero, fmt.Errorf("decode previous request period: %w", err)
				}
				if opts.Period.Kind != "ad_hoc" || prior.RequestRef != opts.Period.RequestRef || !prior.RequestReceivedAt.Equal(opts.Period.RequestReceivedAt) {
					return zero, fleeterror.NewInvalidArgumentError("requested drafts must retain their original request")
				}
			}
			if !previous.CorrectionOf.Valid {
				if err := checkOriginalPeriod(); err != nil {
					return zero, err
				}
			}
			if previous.CorrectionOf.Valid {
				parentID, err := q.HostedReportPublicID(ctx, previous.CorrectionOf.Int64)
				if err != nil {
					return zero, err
				}
				parent, err := q.GetHostedReport(ctx, sqlc.GetHostedReportParams{OrganizationID: orgID, AccountID: a.account.ID, ReportID: parentID})
				if err != nil {
					return zero, accessError(err)
				}
				if !retainsCorrectionPeriod(parent, opts.Period) {
					return zero, fleeterror.NewInvalidArgumentError("correction must retain the original report period")
				}
				latest, err := q.LatestIssuedHostedReport(ctx, sqlc.LatestIssuedHostedReportParams{OrganizationID: orgID, AccountID: a.account.ID, SeriesID: previous.SeriesID})
				if err != nil {
					return zero, err
				}
				if latest.ID != parent.ID {
					return zero, fleeterror.NewFailedPreconditionError("open a correction of the latest issued report")
				}
			}
			row, err = q.RegenerateHostedReport(ctx, sqlc.RegenerateHostedReportParams{ID: previous.ID, Revision: opts.ExpectedRevision, OrganizationID: orgID, Period: periodJSON, PeriodStart: opts.Period.Start, PeriodEnd: opts.Period.End, AsOf: opts.Period.AsOf, Timezone: opts.Period.Timezone, Kind: opts.Period.Kind, InputRevision: opts.InputRevision, Result: resultJSON, SiteIds: sites, CreatedBy: actorID, OwnerID: owner, EvidenceCutoff: evidenceCutoff})
			if errors.Is(err, sql.ErrNoRows) {
				return zero, reportingConflict()
			}
			if err != nil {
				return zero, err
			}
		} else {
			if opts.CorrectionOf == "" {
				if err := checkOriginalPeriod(); err != nil {
					return zero, err
				}
			}
			series := uuid.NewString()
			number := int64(1)
			var correction sql.NullInt64
			if correctionParent != nil {
				series = correctionParent.SeriesID
				number, err = q.NextHostedReportNumber(ctx, sqlc.NextHostedReportNumberParams{OrganizationID: orgID, AccountID: a.account.ID, SeriesID: series})
				if err != nil {
					return zero, err
				}
				correction = sql.NullInt64{Int64: correctionParent.ID, Valid: true}
			}
			row, err = q.CreateHostedReport(ctx, sqlc.CreateHostedReportParams{ReportID: uuid.NewString(), OrganizationID: orgID, AccountID: a.account.ID, SeriesID: series, Number: number, CorrectionOf: correction, CorrectionReason: strings.TrimSpace(opts.CorrectionReason), Kind: opts.Period.Kind, PeriodStart: opts.Period.Start, PeriodEnd: opts.Period.End, AsOf: opts.Period.AsOf, Timezone: opts.Period.Timezone, Period: periodJSON, InputRevision: opts.InputRevision, Result: resultJSON, SiteIds: sites, CreatedBy: actorID, OwnerID: owner, EvidenceCutoff: evidenceCutoff})
			if err != nil {
				return zero, err
			}
		}
		input, err := latestReportInput(ctx, q, a, opts.InputRevision, opts.Period)
		if err != nil {
			return zero, err
		}
		result, err := reporting.CalculateStoredContext(ctx, opts.Period, input.Inputs)
		if err != nil {
			if ctx.Err() != nil {
				return zero, fmt.Errorf("prepare report: %w", ctx.Err())
			}
			return zero, fleeterror.NewInvalidArgumentError(err.Error())
		}
		resultJSON, err = reporting.MarshalResultContext(ctx, result)
		if err != nil {
			if ctx.Err() != nil {
				return zero, fmt.Errorf("prepare report: %w", ctx.Err())
			}
			return zero, fleeterror.NewInvalidArgumentError(err.Error())
		}
		sites = input.Inputs.Sites()
		if sites == nil {
			sites = []int64{}
		}
		identity := reporting.Identity{ReportID: row.ReportID, Version: row.Number, AccountID: accountID, AccountName: a.account.Name, IssuedAt: opts.Period.AsOf, CorrectionReason: row.CorrectionReason}
		if row.CorrectionOf.Valid {
			identity.CorrectionOf, err = q.HostedReportPublicID(ctx, row.CorrectionOf.Int64)
			if err != nil {
				return zero, err
			}
		}
		if err = reporting.PreflightArtifacts(ctx, result, input.Inputs, identity); err != nil {
			if ctx.Err() != nil {
				return zero, fmt.Errorf("prepare report artifacts: %w", ctx.Err())
			}
			return zero, fleeterror.NewFailedPreconditionError("report cannot be published: " + err.Error())
		}
		evidenceSites, err := q.HostedReportEvidenceSites(ctx, row.ID)
		if err != nil {
			return zero, err
		}
		for _, site := range evidenceSites {
			if !site.Valid || !a.sitesAllowed([]int64{site.Int64}) {
				return zero, denied()
			}
			sites = append(sites, site.Int64)
		}
		slices.Sort(sites)
		sites = slices.Compact(sites)
		row, err = q.SetHostedReportContext(ctx, sqlc.SetHostedReportContextParams{ID: row.ID, Result: resultJSON, SiteIds: sites})
		if err != nil {
			return zero, err
		}
		if err = audit(ctx, q, a.actor, a.account, "prepare_report", map[string]any{"report_id": row.ReportID, "revision": row.Revision, "input_revision": input.Revision, "correction_of": opts.CorrectionOf, "correction_reason": opts.CorrectionReason}); err != nil {
			return zero, err
		}
		return loadReportRecord(ctx, q, a.account, row, true)
	})
	return record, reportExecutionError(ctx, err)
}

// A cutoff that outlasts its own budget is waiting for a running hourly
// evidence step, not paying for report scope, so the caller can retry it.
func reportEvidenceCutoffError(ctx context.Context, err error) error {
	if ctx.Err() == nil {
		var pgErr *pgconn.PgError
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &pgErr) && (pgErr.Code == "25P04" || pgErr.Code == "57014")) {
			return fleeterror.NewUnavailableErrorf("hourly monitoring evidence is being recorded; retry report preparation shortly")
		}
	}
	return reportExecutionError(ctx, err)
}

func reportExecutionError(ctx context.Context, err error) error {
	if err != nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded)) {
		return fleeterror.NewFailedPreconditionError("report exceeded its execution time budget; reduce the report scope and try again")
	}
	return err
}

type ReportListOptions struct {
	SeriesID  string
	PageSize  int32
	PageToken string
	Kind      string
	Search    string
}
type ReportPage struct {
	Reports       []ReportRecord
	NextPageToken string
}
type reportListCursor struct {
	Account     string    `json:"account"`
	SeriesID    string    `json:"seriesId"`
	Host        bool      `json:"host"`
	Kind        string    `json:"kind"`
	Search      string    `json:"search"`
	BeforeID    int64     `json:"beforeId"`
	BeforeStart time.Time `json:"beforeStart"`
}

func (s *Service) HostReports(ctx context.Context, actorID, orgID int64, accountID string, options ReportListOptions) (ReportPage, error) {
	return s.reportList(ctx, actorID, orgID, accountID, true, options)
}
func (s *Service) AccountReports(ctx context.Context, actorID, orgID int64, accountID string, options ReportListOptions) (ReportPage, error) {
	return s.reportList(ctx, actorID, orgID, accountID, false, options)
}
func (s *Service) reportList(ctx context.Context, actorID, orgID int64, accountID string, host bool, options ReportListOptions) (ReportPage, error) {
	if len(options.SeriesID) > 100 || options.PageSize < 0 || options.PageSize > 100 || len(options.PageToken) > 2048 || len(options.Search) > 200 || !slices.Contains([]string{"", "monthly", "quarterly", "annual", "ad_hoc"}, options.Kind) {
		return ReportPage{}, fleeterror.NewInvalidArgumentError("invalid report list request")
	}
	size := options.PageSize
	if size == 0 {
		size = 50
	}
	cursor := reportListCursor{Account: accountID, SeriesID: options.SeriesID, Host: host, Kind: options.Kind, Search: options.Search}
	if options.PageToken != "" {
		payload, err := base64.RawURLEncoding.DecodeString(options.PageToken)
		if err != nil || json.Unmarshal(payload, &cursor) != nil || cursor.Account != accountID || cursor.SeriesID != options.SeriesID || cursor.Host != host || cursor.Kind != options.Kind || cursor.Search != options.Search || cursor.BeforeID <= 0 || cursor.BeforeStart.IsZero() {
			return ReportPage{}, fleeterror.NewInvalidArgumentError("invalid report list cursor")
		}
	}
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (ReportPage, error) {
		account, allowed, err := reportReadAdmission(ctx, q, actorID, orgID, accountID, host)
		if err != nil {
			return ReportPage{}, err
		}
		args := sqlc.ListHostedReportSummariesParams{OrganizationID: orgID, AccountID: account.ID, Host: host, SeriesID: options.SeriesID, BeforeID: cursor.BeforeID, BeforeStart: cursor.BeforeStart, Kind: options.Kind, Search: options.Search, PageLimit: size + 1}
		rows := make([]sqlc.ListHostedReportSummariesRow, 0, size+1)
		for len(rows) <= int(size) {
			page, err := q.ListHostedReportSummaries(ctx, args)
			if err != nil {
				return ReportPage{}, err
			}
			for _, row := range page {
				if allowed(row.SiteIds) {
					rows = append(rows, row)
					if len(rows) > int(size) {
						break
					}
				}
			}
			if len(page) < int(args.PageLimit) {
				break
			}
			last := page[len(page)-1]
			args.BeforeID, args.BeforeStart = last.ID, last.PeriodStart
		}
		out := ReportPage{Reports: []ReportRecord{}}
		if len(rows) > int(size) {
			rows = rows[:size]
			last := rows[len(rows)-1]
			cursor.BeforeID = last.ID
			cursor.BeforeStart = last.PeriodStart
			payload, err := json.Marshal(cursor)
			if err != nil {
				return out, fmt.Errorf("encode report cursor: %w", err)
			}
			out.NextPageToken = base64.RawURLEncoding.EncodeToString(payload)
		}
		for _, row := range rows {
			record, err := reportSummaryRecord(account, row)
			if err != nil {
				return out, err
			}
			if !host {
				record = clientReportRecord(record)
			}
			out.Reports = append(out.Reports, record)
		}
		return out, nil
	})
}
func reportSummaryRecord(account Account, row sqlc.ListHostedReportSummariesRow) (ReportRecord, error) {
	record := ReportRecord{ID: row.ID, ReportID: row.ReportID, SeriesID: row.SeriesID, Number: row.Number, Revision: row.Revision, InputRevision: row.InputRevision, Status: row.Status, SiteIDs: row.SiteIds, CreatedBy: row.CreatedBy, OwnerID: row.OwnerID, IssuedAt: reportTime(row.IssuedAt), CorrectionOf: row.CorrectionOf, CorrectionReason: row.CorrectionReason, Exception: row.IncompleteException, DeliveryStatus: row.DeliveryStatus, AccountID: account.AccountID, AccountName: account.Name}
	record.Result.Complete = row.Complete
	if err := json.Unmarshal(row.Period, &record.Period); err != nil {
		return record, fmt.Errorf("decode report period summary: %w", err)
	}
	if err := json.Unmarshal(row.Deadlines, &record.Result.Deadlines); err != nil {
		return record, fmt.Errorf("decode report deadlines: %w", err)
	}
	return record, nil
}
func reportReadAdmission(ctx context.Context, q sqlc.Querier, actorID, orgID int64, accountID string, host bool) (Account, func([]int64) bool, error) {
	if host {
		a, err := reportingHostReadAdmission(ctx, q, actorID, orgID, accountID, "read")
		return a.account, a.sitesAllowed, err
	}
	e, err := loadMiningEstate(ctx, q, actorID, orgID, accountID, false, miningRead)
	return e.account, func(sites []int64) bool {
		for _, site := range sites {
			if !siteAllowed(e.policy, site) || !e.physicalRead(sqlc.ListHostedAccountMiningAssetsRow{SiteID: site}) {
				return false
			}
		}
		return true
	}, err
}
func (s *Service) HostReport(ctx context.Context, actorID, orgID int64, accountID, reportID string) (ReportRecord, error) {
	return s.readReport(ctx, actorID, orgID, accountID, reportID, true)
}
func (s *Service) AccountReport(ctx context.Context, actorID, orgID int64, accountID, reportID string) (ReportRecord, error) {
	return s.readReport(ctx, actorID, orgID, accountID, reportID, false)
}
func (s *Service) readReport(ctx context.Context, actorID, orgID int64, accountID, reportID string, host bool) (ReportRecord, error) {
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (ReportRecord, error) {
		var zero ReportRecord
		account, allowed, err := reportReadAdmission(ctx, q, actorID, orgID, accountID, host)
		if err != nil {
			return zero, err
		}
		row, err := q.GetHostedReport(ctx, sqlc.GetHostedReportParams{OrganizationID: orgID, AccountID: account.ID, ReportID: reportID})
		if err != nil {
			return zero, accessError(err)
		}
		if (!host && row.Status != "issued") || !allowed(row.SiteIds) {
			return zero, denied()
		}
		record, err := loadReportRecord(ctx, q, account, row, host)
		if err != nil {
			return zero, err
		}
		if !host {
			record = clientReportRecord(record)
		}
		return record, nil
	})
}
func clientReportRecord(record ReportRecord) ReportRecord {
	result := record.Result
	result.Obligations = slices.Clone(result.Obligations)
	for i := range result.Obligations {
		result.Obligations[i].TicketRef = ""
	}
	return ReportRecord{ReportID: record.ReportID, SeriesID: record.SeriesID, AccountID: record.AccountID, AccountName: record.AccountName, Number: record.Number, Revision: record.Revision, Period: record.Period, Result: result, Status: record.Status, IssuedAt: record.IssuedAt, CorrectionOf: record.CorrectionOf, CorrectionReason: record.CorrectionReason, Exception: record.Exception, SiteIDs: slices.Clone(record.SiteIDs), DeliveryStatus: record.DeliveryStatus}
}
func reportingEvidenceTimeLimit() time.Time {
	return time.Now().UTC().Add(reportingClockSkew)
}
func nullReportTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}
func (s *Service) RecordReportDelivery(ctx context.Context, actorID, orgID int64, accountID, reportID string, d DeliveryEvidence) error {
	d.SentAt = normalizedReportTime(d.SentAt)
	d.ReceivedAt = normalizedReportTime(d.ReceivedAt)
	limit := reportingEvidenceTimeLimit()
	if (d.SentAt != nil && (d.SentAt.IsZero() || d.SentAt.After(limit))) || (d.ReceivedAt != nil && (d.ReceivedAt.IsZero() || d.ReceivedAt.After(limit))) {
		return fleeterror.NewInvalidArgumentError("delivery timestamps cannot be in the future or zero")
	}
	if _, err := uuid.Parse(d.RequestKey); err != nil {
		return fleeterror.NewInvalidArgumentError("delivery request UUID required")
	}
	if !slices.Contains([]string{"attempted", "failed", reportDeliveryConfirmed}, d.Status) || strings.TrimSpace(d.Channel) == "" || strings.TrimSpace(d.SourceRef) == "" || len(d.SourceRef) > 2000 || len(d.Evidence) > 8000 || len(d.Channel) > 200 || !slices.Contains([]string{"", "unavailable", "rejected", "unconfirmed", "other"}, d.FailureCategory) || (d.Status == "failed" && d.FailureCategory == "") || (d.Status == reportDeliveryConfirmed && (d.SentAt == nil || strings.TrimSpace(d.Evidence) == "")) || (d.ReceivedAt != nil && (d.SentAt == nil || d.ReceivedAt.Before(*d.SentAt))) {
		return fleeterror.NewInvalidArgumentError("invalid delivery evidence")
	}
	return db.WithTransactionTimeoutNoResult(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) error {
		a, err := reportingHostReadAdmission(ctx, q, actorID, orgID, accountID, "read")
		if err != nil {
			return err
		}
		row, err := q.GetHostedReport(ctx, sqlc.GetHostedReportParams{OrganizationID: orgID, AccountID: a.account.ID, ReportID: reportID})
		if err != nil {
			return accessError(err)
		}
		if !a.sitesAllowed(row.SiteIds) || (row.Kind == "ad_hoc" && !a.accountWideRequestsAllowed()) {
			return denied()
		}
		if row.Status != "issued" {
			return reportingConflict()
		}
		if d.SentAt != nil && d.SentAt.Before(row.IssuedAt.Time) {
			return fleeterror.NewInvalidArgumentError("delivery cannot precede this report version's publication")
		}
		_, err = q.CreateHostedReportDelivery(ctx, sqlc.CreateHostedReportDeliveryParams{ReportInternalID: row.ID, RequestKey: d.RequestKey, Channel: d.Channel, Status: d.Status, SentAt: nullReportTime(d.SentAt), ReceivedAt: nullReportTime(d.ReceivedAt), SourceRef: d.SourceRef, Evidence: d.Evidence, FailureCategory: d.FailureCategory, ActorID: actorID})
		if errors.Is(err, sql.ErrNoRows) {
			existing, e := q.GetHostedReportDelivery(ctx, sqlc.GetHostedReportDeliveryParams{ReportInternalID: row.ID, RequestKey: d.RequestKey})
			if e != nil {
				return reportingConflict()
			}
			old := decodeDelivery(existing)
			if old.Channel != d.Channel || old.Status != d.Status || old.SourceRef != d.SourceRef || old.Evidence != d.Evidence || old.FailureCategory != d.FailureCategory || !sameReportTime(old.SentAt, d.SentAt) || !sameReportTime(old.ReceivedAt, d.ReceivedAt) {
				return reportingConflict()
			}
			return nil
		}
		if err != nil {
			return err
		}
		return audit(ctx, q, a.actor, a.account, "record_report_delivery", map[string]any{"report_id": reportID, "request_key": d.RequestKey, "status": d.Status, "channel": d.Channel, "source_ref": d.SourceRef, "failure_category": d.FailureCategory})
	})
}
func normalizedReportTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC().Round(time.Microsecond)
	return &normalized
}

func sameReportTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

type ReportRequest struct {
	FulfilledAt      *time.Time `json:"fulfilledAt,omitempty"`
	FulfillmentRef   string     `json:"fulfillmentRef,omitempty"`
	RequestID        string     `json:"requestId"`
	ReceivedAt       time.Time  `json:"receivedAt"`
	SourceRef        string     `json:"sourceRef"`
	Requester        string     `json:"requester"`
	Scope            string     `json:"scope"`
	OwnerUserID      string     `json:"ownerUserId"`
	TargetAt         time.Time  `json:"targetAt"`
	Limitations      string     `json:"limitations"`
	CommunicationRef string     `json:"communicationRef"`
	DeliveryRef      string     `json:"deliveryRef"`
}

func (s *Service) CreateReportRequest(ctx context.Context, actorID, orgID int64, accountID string, r ReportRequest) (ReportRequest, error) {
	r.ReceivedAt = r.ReceivedAt.UTC().Round(time.Microsecond)
	r.TargetAt = r.TargetAt.UTC().Round(time.Microsecond)
	if r.ReceivedAt.After(reportingEvidenceTimeLimit()) {
		return ReportRequest{}, fleeterror.NewInvalidArgumentError("request receipt cannot be in the future")
	}
	if r.ReceivedAt.IsZero() || strings.TrimSpace(r.SourceRef) == "" || strings.TrimSpace(r.Requester) == "" || strings.TrimSpace(r.Scope) == "" || r.OwnerUserID == "" || (!r.TargetAt.IsZero() && r.TargetAt.Before(r.ReceivedAt)) || len(r.SourceRef) > 2000 || len(r.Requester) > 2000 || len(r.Scope) > 8000 || len(r.Limitations) > 8000 || len(r.CommunicationRef) > 2000 || len(r.DeliveryRef) > 2000 {
		return ReportRequest{}, fleeterror.NewInvalidArgumentError("request receipt, source, requester, scope and owner required")
	}
	if r.TargetAt.IsZero() {
		r.TargetAt = r.ReceivedAt.AddDate(0, 0, 30)
	}
	if r.RequestID != "" {
		return ReportRequest{}, fleeterror.NewInvalidArgumentError("new request cannot supply identity")
	}
	r.RequestID = uuid.NewString()
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (ReportRequest, error) {
		var zero ReportRequest
		a, err := reportingHostReadAdmission(ctx, q, actorID, orgID, accountID, "prepare")
		if err != nil {
			return zero, err
		}
		if !a.accountWideRequestsAllowed() {
			return zero, denied()
		}
		owner, err := q.GetReportingUser(ctx, sqlc.GetReportingUserParams{UserID: r.OwnerUserID, OrganizationID: orgID})
		if err != nil {
			return zero, accessError(err)
		}
		_, err = q.CreateHostedReportRequest(ctx, sqlc.CreateHostedReportRequestParams{RequestID: r.RequestID, OrganizationID: orgID, AccountID: a.account.ID, ReceivedAt: r.ReceivedAt, SourceRef: r.SourceRef, Requester: r.Requester, Scope: r.Scope, OwnerID: owner, TargetAt: r.TargetAt, Limitations: r.Limitations, CommunicationRef: r.CommunicationRef, DeliveryRef: r.DeliveryRef, ActorID: actorID})
		if err != nil {
			return zero, err
		}
		if err = audit(ctx, q, a.actor, a.account, "create_report_request", map[string]any{"request_id": r.RequestID, "received_at": r.ReceivedAt, "source_ref": r.SourceRef, "scope": r.Scope, "owner_id": owner, "target_at": r.TargetAt, "limitations": r.Limitations, "communication_ref": r.CommunicationRef, "delivery_ref": r.DeliveryRef}); err != nil {
			return zero, err
		}
		return r, nil
	})
}

type ReportRequestListOptions struct {
	PageSize  int32
	PageToken string
}
type ReportRequestPage struct {
	Requests      []ReportRequest
	NextPageToken string
}
type reportRequestCursor struct {
	Account          string    `json:"account"`
	BeforeID         int64     `json:"beforeId"`
	BeforeReceivedAt time.Time `json:"beforeReceivedAt"`
}

// ReportRequests pages an account's request history newest first.
func (s *Service) ReportRequests(ctx context.Context, actorID, orgID int64, accountID string, options ReportRequestListOptions) (ReportRequestPage, error) {
	if options.PageSize < 0 || options.PageSize > 100 || len(options.PageToken) > 2048 {
		return ReportRequestPage{}, fleeterror.NewInvalidArgumentError("invalid report request list request")
	}
	size := options.PageSize
	if size == 0 {
		size = 50
	}
	cursor := reportRequestCursor{Account: accountID}
	if options.PageToken != "" {
		payload, err := base64.RawURLEncoding.DecodeString(options.PageToken)
		if err != nil || json.Unmarshal(payload, &cursor) != nil || cursor.Account != accountID || cursor.BeforeID <= 0 || cursor.BeforeReceivedAt.IsZero() {
			return ReportRequestPage{}, fleeterror.NewInvalidArgumentError("invalid report request list cursor")
		}
	}
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (ReportRequestPage, error) {
		a, err := reportingHostReadAdmission(ctx, q, actorID, orgID, accountID, "read")
		if err != nil {
			return ReportRequestPage{}, err
		}
		if !a.accountWideRequestsAllowed() {
			return ReportRequestPage{Requests: []ReportRequest{}}, nil
		}
		rows, err := q.ListHostedReportRequests(ctx, sqlc.ListHostedReportRequestsParams{OrganizationID: orgID, AccountID: a.account.ID, BeforeID: cursor.BeforeID, BeforeReceivedAt: cursor.BeforeReceivedAt, PageLimit: size + 1})
		if err != nil {
			return ReportRequestPage{}, err
		}
		out := ReportRequestPage{Requests: make([]ReportRequest, 0, min(len(rows), int(size)))}
		if len(rows) > int(size) {
			rows = rows[:size]
			last := rows[len(rows)-1]
			cursor.BeforeID, cursor.BeforeReceivedAt = last.ID, last.ReceivedAt
			payload, err := json.Marshal(cursor)
			if err != nil {
				return out, fmt.Errorf("encode report request cursor: %w", err)
			}
			out.NextPageToken = base64.RawURLEncoding.EncodeToString(payload)
		}
		for _, row := range rows {
			out.Requests = append(out.Requests, ReportRequest{RequestID: row.RequestID, ReceivedAt: row.ReceivedAt, SourceRef: row.SourceRef, Requester: row.Requester, Scope: row.Scope, OwnerUserID: row.OwnerUserID, TargetAt: row.TargetAt, Limitations: row.Limitations, CommunicationRef: row.CommunicationRef, DeliveryRef: row.DeliveryRef, FulfilledAt: requestFulfilledAt(row.FulfilledAt), FulfillmentRef: row.FulfillmentRef})
		}
		return out, nil
	})
}

type ReportRequestFulfillment struct {
	EntryKey         string
	DeliveredAt      time.Time
	DeliveryRef      string
	CommunicationRef string
	Evidence         string
}

func (s *Service) RecordReportRequestFulfillment(ctx context.Context, actorID, orgID int64, accountID, requestID string, entry ReportRequestFulfillment) error {
	if _, err := uuid.Parse(entry.EntryKey); err != nil {
		return fleeterror.NewInvalidArgumentError("fulfillment entry UUID required")
	}
	entry.DeliveredAt = entry.DeliveredAt.UTC().Round(time.Microsecond)
	if entry.DeliveredAt.IsZero() || entry.DeliveredAt.After(reportingEvidenceTimeLimit()) || strings.TrimSpace(entry.DeliveryRef) == "" || len(entry.DeliveryRef) > 2000 || len(entry.CommunicationRef) > 2000 || strings.TrimSpace(entry.Evidence) == "" || len(entry.Evidence) > 8000 {
		return fleeterror.NewInvalidArgumentError("actual delivery time, reference and evidence required")
	}
	return db.WithTransactionTimeoutNoResult(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) error {
		a, err := reportingHostAdmission(ctx, q, actorID, orgID, accountID, "prepare")
		if err != nil {
			return err
		}
		if !a.accountWideRequestsAllowed() {
			return denied()
		}
		request, err := q.GetHostedReportRequest(ctx, sqlc.GetHostedReportRequestParams{OrganizationID: orgID, AccountID: a.account.ID, RequestID: requestID})
		if err != nil {
			return accessError(err)
		}
		if entry.DeliveredAt.Before(request.ReceivedAt) {
			return fleeterror.NewInvalidArgumentError("fulfillment cannot precede request receipt")
		}
		_, err = q.CreateHostedReportRequestFulfillment(ctx, sqlc.CreateHostedReportRequestFulfillmentParams{RequestInternalID: request.ID, EntryKey: entry.EntryKey, DeliveredAt: entry.DeliveredAt, DeliveryRef: entry.DeliveryRef, CommunicationRef: entry.CommunicationRef, Evidence: entry.Evidence, ActorID: actorID})
		if errors.Is(err, sql.ErrNoRows) {
			old, e := q.GetHostedReportRequestFulfillment(ctx, sqlc.GetHostedReportRequestFulfillmentParams{RequestInternalID: request.ID, EntryKey: entry.EntryKey})
			if e != nil {
				return e
			}
			if !old.DeliveredAt.Equal(entry.DeliveredAt) || old.DeliveryRef != entry.DeliveryRef || old.CommunicationRef != entry.CommunicationRef || old.Evidence != entry.Evidence {
				return reportingConflict()
			}
			return nil
		}
		if err != nil {
			return err
		}
		return audit(ctx, q, a.actor, a.account, "fulfill_report_request", map[string]any{"request_id": requestID, "entry_key": entry.EntryKey, "delivery_ref": entry.DeliveryRef, "communication_ref": entry.CommunicationRef, "delivered_at": entry.DeliveredAt})
	})
}

func retainsCorrectionPeriod(parent sqlc.HostedReport, candidate reporting.Period) bool {
	if parent.Kind != candidate.Kind || !parent.PeriodStart.Equal(candidate.Start) || !parent.PeriodEnd.Equal(candidate.End) || parent.Timezone != candidate.Timezone {
		return false
	}
	if parent.Kind == "ad_hoc" {
		var original reporting.Period
		if json.Unmarshal(parent.Period, &original) != nil {
			return false
		}
		return original.RequestRef == candidate.RequestRef && original.RequestReceivedAt.Equal(candidate.RequestReceivedAt)
	}
	return true
}

func requestFulfilledAt(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
