package hostedaccounts

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

const reportArtifactChunkSize = 64 * 1024
const reportEvidenceIntervalPageSize = 512

type reportArtifactWriter struct {
	appendChunk    func(sequence int64, data []byte) error
	q              sqlc.Querier
	artifactID     int64
	digest         hash.Hash
	buffer         []byte
	size, sequence int64
	failure        error
}

func newReportArtifactWriter(ctx context.Context, q sqlc.Querier, id int64) *reportArtifactWriter {
	return &reportArtifactWriter{q: q, artifactID: id, digest: sha256.New(), buffer: make([]byte, 0, reportArtifactChunkSize), appendChunk: func(sequence int64, data []byte) error {
		return q.AppendHostedReportArtifactChunk(ctx, sqlc.AppendHostedReportArtifactChunkParams{ArtifactID: id, Sequence: sequence, Data: data})
	}}
}
func (w *reportArtifactWriter) Write(data []byte) (int, error) {
	if w.failure != nil {
		return 0, w.failure
	}
	accepted := 0
	for len(data) > 0 {
		count := min(reportArtifactChunkSize-len(w.buffer), len(data))
		part := data[:count]
		w.buffer = append(w.buffer, part...)
		_, _ = w.digest.Write(part)
		w.size += int64(count)
		accepted += count
		data = data[count:]
		if len(w.buffer) == reportArtifactChunkSize {
			if err := w.flush(); err != nil {
				w.failure = err
				return accepted, err
			}
		}
	}
	return accepted, nil
}
func (w *reportArtifactWriter) flush() error {
	if len(w.buffer) == 0 {
		return nil
	}
	if err := w.appendChunk(w.sequence, w.buffer); err != nil {
		return err
	}
	w.sequence++
	w.buffer = w.buffer[:0]
	return nil
}
func (w *reportArtifactWriter) finish(ctx context.Context) error {
	if w.failure != nil {
		return w.failure
	}
	if err := w.flush(); err != nil {
		return err
	}
	changed, err := w.q.FinalizeHostedReportArtifact(ctx, sqlc.FinalizeHostedReportArtifactParams{ID: w.artifactID, Sha256: hex.EncodeToString(w.digest.Sum(nil)), ByteCount: w.size, ChunkCount: w.sequence})
	if err != nil {
		return err
	}
	if changed != 1 {
		return reportingConflict()
	}
	return nil
}
func reportNumber(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}
func reportFlag(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	return &value.Bool
}

// The exported source window spans every metric of the hour.
func reportSourceWindow(oldest, newest []sql.NullTime) (time.Time, time.Time) {
	var first, last time.Time
	for _, at := range oldest {
		if at.Valid && (first.IsZero() || at.Time.Before(first)) {
			first = at.Time
		}
	}
	for _, at := range newest {
		if at.Valid && at.Time.After(last) {
			last = at.Time
		}
	}
	return first, last
}
func reportObservation(interval sqlc.ListHostedReportEvidenceIntervalsRow, row sqlc.ListHostedReportEvidenceRow) reporting.Observation {
	oldest, newest := reportSourceWindow(
		[]sql.NullTime{row.HashrateSourceOldestAt, row.PowerSourceOldestAt, row.TemperatureSourceOldestAt, row.StateSourceOldestAt},
		[]sql.NullTime{row.HashrateSourceNewestAt, row.PowerSourceNewestAt, row.TemperatureSourceNewestAt, row.StateSourceNewestAt})
	return reporting.Observation{IntervalID: interval.IntervalID, AssetID: interval.AssetID, SiteID: interval.SiteID.Int64, Kind: interval.Kind,
		Bucket: row.Bucket, Revision: row.Revision, CapturedAt: row.CapturedAt, CaptureReason: row.CaptureReason, GapReason: row.GapReason,
		NoSourceData: row.NoSourceData, WindowStart: row.WindowStart.Time, WindowEnd: row.WindowEnd.Time,
		ObservationValues: reporting.ObservationValues{
			HashrateReports: int64(row.HashrateReportCount), HashrateCount: int64(row.HashrateCount), HashratePositive: int64(row.HashratePositiveCount),
			HashrateSum: reportNumber(row.HashrateSum), HashrateComplete: reportFlag(row.HashrateComplete),
			PowerReports: int64(row.PowerReportCount), PowerCount: int64(row.PowerCount), PowerSum: reportNumber(row.PowerSum), PowerComplete: reportFlag(row.PowerComplete),
			TemperatureReports: int64(row.TemperatureReportCount), TemperatureCount: int64(row.TemperatureCount), TemperatureSum: reportNumber(row.TemperatureSum),
			TemperatureLatest: reportNumber(row.TemperatureLatestValue), TemperatureLatestAt: row.TemperatureLatestAt.Time,
			StateReports: int64(row.StateReportCount), StateCount: int64(row.StateCount), StateLatest: reportNumber(row.StateLatestValue), StateLatestAt: row.StateLatestAt.Time,
			SourceOldestAt: oldest, SourceNewestAt: newest}}
}
func reportObservationDay(interval sqlc.ListHostedReportEvidenceIntervalsRow, row sqlc.ListHostedReportEvidenceDaysRow) reporting.ObservationDay {
	return reporting.ObservationDay{IntervalID: interval.IntervalID, AssetID: interval.AssetID, SiteID: interval.SiteID.Int64, Kind: interval.Kind,
		Day: row.Day.Time, Hours: row.EvidenceHours.Int32, GapHours: row.GapHours.Int32, RevisedHours: row.RevisedHours.Int32,
		UnattributableHours: row.UnattributableHours.Int32, NoSourceDataHours: row.NoSourceDataHours.Int32,
		Revision: row.Revision.Int32, CapturedAt: row.CapturedAt.Time, WindowStart: row.WindowStart.Time, WindowEnd: row.WindowEnd.Time,
		ObservationValues: reporting.ObservationValues{
			HashrateReports: row.HashrateReportCount.Int64, HashrateCount: row.HashrateCount.Int64, HashratePositive: row.HashratePositiveCount.Int64,
			HashrateSum: reportNumber(row.HashrateSum), HashrateComplete: reportFlag(row.HashrateComplete),
			PowerReports: row.PowerReportCount.Int64, PowerCount: row.PowerCount.Int64, PowerSum: reportNumber(row.PowerSum), PowerComplete: reportFlag(row.PowerComplete),
			TemperatureReports: row.TemperatureReportCount.Int64, TemperatureCount: row.TemperatureCount.Int64, TemperatureSum: reportNumber(row.TemperatureSum),
			TemperatureLatest: reportNumber(row.TemperatureLatestValue), TemperatureLatestAt: row.TemperatureLatestAt.Time,
			StateReports: row.StateReportCount.Int64, StateCount: row.StateCount.Int64, StateLatest: reportNumber(row.StateLatestValue), StateLatestAt: row.StateLatestAt.Time,
			SourceOldestAt: row.SourceOldestAt.Time, SourceNewestAt: row.SourceNewestAt.Time}}
}

// reportEvidence reads a draft's frozen monitoring evidence one allocated
// interval at a time, in (interval, time) order. The site of every interval
// with evidence must be visible to the publishing actor.
func reportEvidence(ctx context.Context, q sqlc.Querier, reportID int64, sitesAllowed func([]int64) bool) reporting.ObservationSource {
	visible := func(interval sqlc.ListHostedReportEvidenceIntervalsRow) error {
		if !interval.SiteID.Valid || !sitesAllowed([]int64{interval.SiteID.Int64}) {
			return denied()
		}
		return nil
	}
	intervals := func(visit func(sqlc.ListHostedReportEvidenceIntervalsRow) error) error {
		after := int64(0)
		for {
			page, err := q.ListHostedReportEvidenceIntervals(ctx, sqlc.ListHostedReportEvidenceIntervalsParams{ReportInternalID: reportID, AfterIntervalID: after, PageLimit: reportEvidenceIntervalPageSize})
			if err != nil {
				return err
			}
			for _, interval := range page {
				if err := visit(interval); err != nil {
					return err
				}
				after = interval.IntervalID
			}
			if len(page) < reportEvidenceIntervalPageSize {
				return nil
			}
		}
	}
	return reporting.ObservationSource{
		Hours: func(yield func(reporting.Observation) error) error {
			return intervals(func(interval sqlc.ListHostedReportEvidenceIntervalsRow) error {
				hours, err := q.ListHostedReportEvidence(ctx, sqlc.ListHostedReportEvidenceParams{ReportInternalID: reportID, IntervalID: interval.IntervalID})
				if err != nil {
					return err
				}
				for _, hour := range hours {
					if err := visible(interval); err != nil {
						return err
					}
					if err := yield(reportObservation(interval, hour)); err != nil {
						return err
					}
				}
				return nil
			})
		},
		Days: func(yield func(reporting.ObservationDay) error) error {
			return intervals(func(interval sqlc.ListHostedReportEvidenceIntervalsRow) error {
				days, err := q.ListHostedReportEvidenceDays(ctx, sqlc.ListHostedReportEvidenceDaysParams{ReportInternalID: reportID, IntervalID: interval.IntervalID})
				if err != nil {
					return err
				}
				for _, day := range days {
					if err := visible(interval); err != nil {
						return err
					}
					if err := yield(reportObservationDay(interval, day)); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
}

func (s *Service) PublishReport(ctx context.Context, actorID, orgID int64, accountID, reportID string, expected int64, exception string) (ReportRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, reportingTransactionTimeout)
	defer cancel()
	if expected <= 0 || len(exception) > 4000 {
		return ReportRecord{}, fleeterror.NewInvalidArgumentError("publication revision and bounded exception required")
	}
	record, err := db.WithTransactionTimeout(ctx, s.conn, reportingTransactionTimeout, func(q sqlc.Querier) (ReportRecord, error) {
		var zero ReportRecord
		a, err := reportingHostReadAdmission(ctx, q, actorID, orgID, accountID, "publish")
		if err != nil {
			return zero, err
		}
		if _, err := q.LockReportingInputsForReport(ctx, sqlc.LockReportingInputsForReportParams{OrganizationID: orgID, AccountID: a.account.ID}); err != nil {
			return zero, accessError(err)
		}
		row, err := q.LockHostedReport(ctx, sqlc.LockHostedReportParams{OrganizationID: orgID, AccountID: a.account.ID, ReportID: reportID})
		if err != nil {
			return zero, accessError(err)
		}
		if row.Status != "draft" || row.Revision != expected {
			return zero, reportingConflict()
		}
		if !a.sitesAllowed(row.SiteIds) {
			return zero, denied()
		}
		record, err := loadReportRecord(ctx, q, a.account, row, true)
		if err != nil {
			return zero, err
		}
		inputs, err := latestReportInput(ctx, q, a, record.InputRevision, record.Period)
		if err != nil {
			return zero, err
		}
		recalculated, err := reporting.CalculateStoredContext(ctx, record.Period, inputs.Inputs)
		if err != nil {
			return zero, fmt.Errorf("recalculate report for publication: %w", err)
		}
		if err := reporting.VerifySavedReviewedCalculation(ctx, recalculated, record.Result); err != nil {
			return zero, fleeterror.NewFailedPreconditionError("reviewed report differs from current report context; regenerate before publication")
		}
		if !record.Result.Complete && strings.TrimSpace(exception) == "" {
			return zero, fleeterror.NewFailedPreconditionError("incomplete reports require an explicit publication exception")
		}
		if row.CorrectionOf.Valid {
			latest, err := q.LatestIssuedHostedReport(ctx, sqlc.LatestIssuedHostedReportParams{OrganizationID: orgID, AccountID: a.account.ID, SeriesID: row.SeriesID})
			if err != nil {
				return zero, err
			}
			if latest.Number >= row.Number || latest.ID != row.CorrectionOf.Int64 || !retainsCorrectionPeriod(latest, record.Period) {
				return zero, reportingConflict()
			}
		}
		if !row.CorrectionOf.Valid && slices.Contains([]string{"monthly", "quarterly", "annual"}, row.Kind) {
			exists, err := q.HasIssuedHostedOriginalReport(ctx, sqlc.HasIssuedHostedOriginalReportParams{OrganizationID: orgID, AccountID: a.account.ID, Kind: row.Kind, PeriodStart: row.PeriodStart, PeriodEnd: row.PeriodEnd})
			if err != nil {
				return zero, err
			}
			if exists {
				return zero, fleeterror.NewFailedPreconditionError("an original report has already been issued for this period; prepare a correction")
			}
		}
		issuedAt, err := q.SelectHostedReportPublicationTime(ctx)
		if err != nil {
			return zero, err
		}
		identity := reporting.Identity{ReportID: record.ReportID, Version: record.Number, AccountID: accountID, AccountName: a.account.Name, IssuedAt: issuedAt, CorrectionOf: record.CorrectionOf, CorrectionReason: record.CorrectionReason, Exception: strings.TrimSpace(exception)}

		// The draft's evidence cutoff, not a new telemetry query, defines the frozen
		// observation stream. The evidence archive replays it once under this
		// transaction, rechecking current site authority, and returns the public
		// evidence it selected.
		var selected reporting.Inputs

		for _, file := range []struct {
			name, media string
			render      func(io.Writer) error
		}{
			{"source-inputs.json", "application/json", func(w io.Writer) error {
				return writeReportingContextSource(ctx, w, q, orgID, a.account.ID, record.Period, inputs)
			}},
			{"report.html", "text/html; charset=utf-8", func(w io.Writer) error {
				return reporting.WriteWrittenReport(ctx, w, record.Result, identity)
			}},
			{"evidence.zip", reporting.PublicEvidenceMediaType, func(w io.Writer) error {
				var err error
				selected, err = reporting.WriteSelectedEvidenceContext(ctx, w, record.Result, inputs.Inputs, identity, reportEvidence(ctx, q, row.ID, a.sitesAllowed))
				return err
			}},
		} {
			artifact, err := q.CreateHostedReportArtifact(ctx, sqlc.CreateHostedReportArtifactParams{ReportInternalID: row.ID, Name: file.name, MediaType: file.media})
			if err != nil {
				return zero, err
			}
			writer := newReportArtifactWriter(ctx, q, artifact.ID)
			if err = file.render(writer); err != nil {
				if errors.Is(err, reporting.ErrOutputLimit) || errors.Is(err, reporting.ErrPublicReference) {
					return zero, fleeterror.NewFailedPreconditionError("report cannot be published: " + err.Error())
				}
				return zero, fmt.Errorf("render frozen report artifact: %w", err)
			}
			if err = writer.finish(ctx); err != nil {
				return zero, err
			}
		}
		// Check parity on the selection the archive used. A mismatch rolls back
		// every artifact written above, so publication still fails closed.
		if err := reporting.VerifyPublicEvidenceCalculation(ctx, record.Period, selected, recalculated); err != nil {
			return zero, fmt.Errorf("verify public report evidence: %w", err)
		}
		issued, err := q.IssueHostedReport(ctx, sqlc.IssueHostedReportParams{ID: row.ID, Revision: expected, IssuedBy: sql.NullInt64{Int64: actorID, Valid: true}, IncompleteException: strings.TrimSpace(exception), IssuedAt: sql.NullTime{Time: issuedAt, Valid: true}})
		if errors.Is(err, sql.ErrNoRows) {
			return zero, reportingConflict()
		}
		if err != nil {
			return zero, err
		}
		if err = audit(ctx, q, a.actor, a.account, "publish_report", map[string]any{"report_id": reportID, "number": row.Number, "revision": issued.Revision, "exception": strings.TrimSpace(exception)}); err != nil {
			return zero, err
		}
		return loadReportRecord(ctx, q, a.account, issued, true)
	})
	return record, reportExecutionError(ctx, err)
}

type ReportArtifactChunk struct {
	Data                 []byte
	Name                 string
	MediaType            string
	SHA256               string
	Bytes                int64
	Chunks               int64
	AuthorityFingerprint string
}

// Each chunk receives fresh live admission. No database lock is held while the
// handler waits for a client to consume a network message.
func (s *Service) ReportArtifactPage(ctx context.Context, actorID, orgID int64, accountID, reportID, name string, sequence int64, host bool) (ReportArtifactChunk, error) {
	if name == "source-inputs.json" && !host {
		return ReportArtifactChunk{}, denied()
	}
	if sequence < 0 || !slices.Contains([]string{"report.html", "evidence.zip", "source-inputs.json"}, name) {
		return ReportArtifactChunk{}, fleeterror.NewInvalidArgumentError("invalid report artifact page")
	}
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (ReportArtifactChunk, error) {
		var out ReportArtifactChunk
		account, allowed, err := reportReadAdmission(ctx, q, actorID, orgID, accountID, host)
		if err != nil {
			return out, err
		}
		report, err := q.GetHostedReportArtifactScope(ctx, sqlc.GetHostedReportArtifactScopeParams{OrganizationID: orgID, AccountID: account.ID, ReportID: reportID})
		if err != nil {
			return out, accessError(err)
		}
		if report.Status != "issued" || !allowed(report.SiteIds) {
			return out, denied()
		}
		artifact, err := q.GetHostedReportArtifact(ctx, sqlc.GetHostedReportArtifactParams{OrganizationID: orgID, AccountID: account.ID, ReportID: reportID, Name: name})
		if errors.Is(err, sql.ErrNoRows) {
			return out, fleeterror.NewNotFoundError("report artifact not found")
		}
		if err != nil {
			return out, err
		}
		if !host {
			profile := artifact.MediaType
			if name == "report.html" {
				evidence, err := q.GetHostedReportArtifact(ctx, sqlc.GetHostedReportArtifactParams{OrganizationID: orgID, AccountID: account.ID, ReportID: reportID, Name: "evidence.zip"})
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return out, err
				}
				profile = evidence.MediaType
			}
			if profile != reporting.PublicEvidenceMediaType {
				return out, fleeterror.NewFailedPreconditionError("Prepare and publish a correction to provide client-safe evidence")
			}
		}
		if sequence >= artifact.ChunkCount {
			return out, fleeterror.NewInvalidArgumentError("report artifact page is outside its frozen length")
		}
		path, err := loadPath(ctx, q, orgID, accountID)
		if err != nil {
			return out, err
		}
		fingerprint := struct {
			Path               []Account
			MembershipRevision int64
			HostGrantRevision  int64
		}{Path: path}
		if host {
			grant, err := q.GetReportingAuthority(ctx, sqlc.GetReportingAuthorityParams{OrganizationID: orgID, AccountID: account.ID, UserID: actorID})
			if err != nil {
				return out, accessError(err)
			}
			fingerprint.HostGrantRevision = grant.Revision
		} else {
			membership, err := q.GetHostedAccountMembership(ctx, sqlc.GetHostedAccountMembershipParams{OrganizationID: orgID, AccountID: account.ID, UserID: actorID})
			if err == nil {
				fingerprint.MembershipRevision = membership.Revision
			} else if !errors.Is(err, sql.ErrNoRows) {
				return out, err
			}
		}
		encoded, err := json.Marshal(fingerprint)
		if err != nil {
			return out, fmt.Errorf("encode report authority fingerprint: %w", err)
		}
		digest := sha256.Sum256(encoded)
		data, err := q.GetHostedReportArtifactChunk(ctx, sqlc.GetHostedReportArtifactChunkParams{ArtifactID: artifact.ID, Sequence: sequence})
		if err != nil {
			return out, err
		}
		out = ReportArtifactChunk{Data: data, Name: artifact.Name, MediaType: artifact.MediaType, SHA256: artifact.Sha256, Bytes: artifact.ByteCount, Chunks: artifact.ChunkCount, AuthorityFingerprint: hex.EncodeToString(digest[:])}
		return out, nil
	})
}
