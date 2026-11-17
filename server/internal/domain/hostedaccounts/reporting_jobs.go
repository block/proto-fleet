package hostedaccounts

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/alerts"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
)

// ReportQueueState contains workflow facts only; alert payloads never contain report contents.
type ReportQueueState struct {
	ActionPath     string              `json:"actionPath"`
	Kind           string              `json:"kind"`
	Period         reporting.Period    `json:"period"`
	OwnerUserID    string              `json:"ownerUserId"`
	ReportID       string              `json:"reportId,omitempty"`
	InputRevision  int64               `json:"inputRevision"`
	ReportRevision int64               `json:"reportRevision,omitempty"`
	ReportStatus   string              `json:"reportStatus,omitempty"`
	Complete       bool                `json:"complete"`
	Request        *ReportRequest      `json:"request,omitempty"`
	Deadlines      reporting.Deadlines `json:"deadlines"`
	Reasons        []string            `json:"reasons"`
	DeliveryStatus string              `json:"deliveryStatus"`
}
type ReportQueueEvent struct {
	Generation  int64
	Kind        string
	ActorUserID string
	Reason      string
	CreatedAt   time.Time
}
type ReportQueueItem struct {
	ReportQueueState
	QueueID            string
	AccountID          string
	OwnerUsername      string
	AccountName        string
	Generation         int64
	Attempts           int64
	NotificationStatus string
	Events             []ReportQueueEvent
	Report             *ReportRecord
	CanAcknowledge     bool
	CanRetry           bool
}
type ReportingNotifier interface {
	DeliverTrackedSkipping(ctx context.Context, batch []alerts.Alert, deliveredChannels []int64) alerts.DeliveryResult
}
type ReportingJobs struct {
	service  *Service
	notifier ReportingNotifier
}

func NewReportingJobs(service *Service, notifier ReportingNotifier) *ReportingJobs {
	return &ReportingJobs{service: service, notifier: notifier}
}

func (j *ReportingJobs) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := j.Tick(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
			slog.Error("hosted reporting job failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick uses a durable claim before crossing the notification boundary. Provider
// acceptance followed by process loss can cause a retry: delivery is at least once.
func (j *ReportingJobs) Tick(ctx context.Context, now time.Time) error {
	now = now.UTC().Round(time.Microsecond)
	tickStarted := time.Now()
	discoveryCtx, cancelDiscovery := context.WithTimeout(ctx, miningTransactionTimeout)
	accounts, err := j.service.queries.ListReportingJobAccounts(discoveryCtx)
	cancelDiscovery()
	if err != nil {
		return err
	}
	var failures []error
	finish := func(err error) error { return errors.Join(append(failures, err)...) }
	for _, account := range accounts {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		if err := j.materializeAccount(ctx, account, now); err != nil {
			failures = append(failures, fmt.Errorf("materialize reporting account %s: %w", account.AccountID, err))
		}
	}
	// A lost final claim cannot be claimed again. Release its lease and retain
	// an actionable terminal event even when notification delivery is disabled.
	err = db.WithTransactionTimeoutNoResult(ctx, j.service.conn, miningTransactionTimeout, func(q sqlc.Querier) error {
		rows, err := q.ExhaustExpiredHostedReportQueueNotifications(ctx, now)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := q.AddHostedReportQueueEvent(ctx, sqlc.AddHostedReportQueueEventParams{QueueInternalID: row.ID, Generation: row.Generation, Kind: "exhausted", Reason: "automatic retries exhausted after notification claim expired; review destinations before retrying", CreatedAt: now}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return finish(err)
	}
	if j.notifier == nil {
		return finish(ctx.Err())
	}
	for range 32 {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		now = now.Add(time.Since(tickStarted))
		tickStarted = time.Now()
		claim, err := db.WithTransactionTimeout(ctx, j.service.conn, miningTransactionTimeout, func(q sqlc.Querier) (sqlc.HostedReportQueue, error) {
			row, err := q.ClaimHostedReportQueueNotification(ctx, sqlc.ClaimHostedReportQueueNotificationParams{NowAt: now, ClaimToken: sql.NullString{String: uuid.NewString(), Valid: true}, LeaseUntil: sql.NullTime{Time: now.Add(2 * time.Minute), Valid: true}})
			if err != nil {
				return row, err
			}
			return row, q.AddHostedReportQueueEvent(ctx, sqlc.AddHostedReportQueueEventParams{QueueInternalID: row.ID, Generation: row.Generation, Kind: "attempted", Reason: "notification delivery attempt", CreatedAt: now})
		})
		if errors.Is(err, sql.ErrNoRows) {
			return finish(nil)
		}
		if err != nil {
			return finish(err)
		}
		var state ReportQueueState
		if err = json.Unmarshal(claim.Projection, &state); err != nil {
			return finish(err)
		}
		status, reason := "blocked", "reporting owner no longer has authority for queue item"
		deliveredChannels := append([]int64{}, claim.DeliveredChannelIds...)
		admitted, err := db.WithTransactionTimeout(ctx, j.service.conn, miningTransactionTimeout, func(q sqlc.Querier) (bool, error) {
			// Shared admission still excludes hierarchy, membership and authority
			// changes without making the organization's readers wait.
			if _, err := q.LockHostedAccountOrganizationForShare(ctx, claim.OrganizationID); err != nil {
				return false, err
			}
			account, err := q.GetReportingJobAccount(ctx, sqlc.GetReportingJobAccountParams{OrganizationID: claim.OrganizationID, ID: claim.AccountID})
			if err != nil {
				return false, err
			}
			a, err := reportingHostReadAdmission(ctx, q, claim.OwnerID, claim.OrganizationID, account.AccountID, "read")
			if fleeterror.IsForbiddenError(err) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			if !a.sitesAllowed(claim.SiteIds) || (state.Request != nil && !a.accountWideRequestsAllowed()) {
				return false, nil
			}
			current, err := q.GetHostedReportQueue(ctx, sqlc.GetHostedReportQueueParams{OrganizationID: claim.OrganizationID, AccountID: claim.AccountID, QueueID: claim.QueueID})
			if err != nil {
				return false, err
			}
			return current.Generation == claim.Generation && current.ClaimToken == claim.ClaimToken && current.LeaseUntil.Valid && current.LeaseUntil.Time.After(now.Add(time.Since(tickStarted))), nil
		})
		if err != nil {
			return finish(err)
		}
		if admitted {
			sendCtx, cancelSend := context.WithTimeout(ctx, time.Minute)
			result := j.notifier.DeliverTrackedSkipping(sendCtx, []alerts.Alert{queueAlert(claim, state)}, deliveredChannels)
			cancelSend()
			deliveredChannels = append(deliveredChannels, result.DeliveredChannelIDs...)
			slices.Sort(deliveredChannels)
			deliveredChannels = slices.Compact(deliveredChannels)
			status, reason = "sent", "notification accepted by configured destinations"
			if result.Failed > 0 {
				status, reason = "failed", "notification not accepted by all configured destinations"
				if claim.Attempts >= 5 {
					status, reason = "exhausted", "automatic retries exhausted; review destinations before retrying"
				}
			} else if result.Delivered == 0 && len(deliveredChannels) == 0 {
				status, reason = "unrouted", "no destination accepted the notification; review routing before retrying"
			}
		}
		completedAt := now.Add(time.Since(tickStarted))
		if ctx.Err() != nil {
			return finish(ctx.Err())
		}
		err = db.WithTransactionTimeoutNoResult(ctx, j.service.conn, miningTransactionTimeout, func(q sqlc.Querier) error {
			changed, err := q.CompleteHostedReportQueueNotification(ctx, sqlc.CompleteHostedReportQueueNotificationParams{ID: claim.ID, Generation: claim.Generation, ClaimToken: claim.ClaimToken, NotificationStatus: status, RetryAt: completedAt.Add(notificationBackoff(claim.Attempts)), DeliveredChannelIds: deliveredChannels, NowAt: completedAt})
			if err != nil {
				return err
			}
			if changed == 0 {
				return nil
			}
			return q.AddHostedReportQueueEvent(ctx, sqlc.AddHostedReportQueueEventParams{QueueInternalID: claim.ID, Generation: claim.Generation, Kind: status, Reason: reason, CreatedAt: completedAt})
		})
		if err != nil {
			return finish(err)
		}
	}
	return finish(nil)
}
func notificationBackoff(attempt int64) time.Duration {
	switch attempt {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	case 3:
		return 15 * time.Minute
	default:
		return time.Hour
	}
}
func periodWorkKey(p reporting.Period) string {
	return fmt.Sprintf("%s:%s:%s", p.Kind, p.Start.UTC().Format(time.RFC3339Nano), p.End.UTC().Format(time.RFC3339Nano))
}
func queueAlert(row sqlc.HostedReportQueue, state ReportQueueState) alerts.Alert {
	summary := "Hosted reporting work requires its accountable owner's attention."
	switch {
	case slices.Contains(state.Reasons, "delivery_failed"):
		summary = "Hosted report delivery failed. Open the queue item to retry or record delivery evidence."
	case slices.Contains(state.Reasons, "contract_overdue"):
		summary = "A hosted report is past its approved contractual due date. Open the queue item to act."
	case slices.Contains(state.Reasons, "internal_target_overdue"):
		summary = "A hosted report has passed a proposed internal target. Open the queue item to act."
	case slices.Contains(state.Reasons, "draft"):
		summary = "A hosted report draft is ready to publish."
	case slices.Contains(state.Reasons, "capture_missing") || slices.Contains(state.Reasons, "capture_lag") || slices.Contains(state.Reasons, "source_lag"):
		summary = "Hosted reporting observations are missing or delayed. Open the queue item to investigate."
	}
	return alerts.Alert{Status: "firing", RuleUID: "hosted-reporting-operations", Labels: map[string]string{"organization_id": fmt.Sprint(row.OrganizationID), "alertname": "Hosted report requires action", "severity": "warning", "rule_group": "hosted-reporting", "proto_fleet_rule_uid": "hosted-reporting-operations", "report_queue_id": row.QueueID}, Annotations: map[string]string{"summary": summary, "action_path": state.ActionPath + row.QueueID}}
}
func (j *ReportingJobs) materializeAccount(ctx context.Context, account Account, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, reportingTransactionTimeout)
	defer cancel()
	return db.WithTransactionTimeoutNoResult(ctx, j.service.conn, reportingTransactionTimeout, func(q sqlc.Querier) error {
		// Shared organization admission excludes hierarchy changes while other
		// accounts' readers and workers proceed. The account lock serializes
		// workers, and the inputs lock excludes this account's report and fact
		// writers, so one account projection is still materialized atomically.
		if _, err := q.LockHostedAccountOrganizationForShare(ctx, account.OrganizationID); err != nil {
			return err
		}
		if err := q.LockHostedReportQueueAccount(ctx, account.ID); err != nil {
			return err
		}
		path, err := loadPath(ctx, q, account.OrganizationID, account.AccountID)
		if err != nil {
			return err
		}
		for _, member := range path {
			if member.Suspended {
				return nil
			}
		}
		inputs, inputsErr := q.LockReportingInputsForShare(ctx, sqlc.LockReportingInputsForShareParams{OrganizationID: account.OrganizationID, AccountID: account.ID})
		if inputsErr != nil && !errors.Is(inputsErr, sql.ErrNoRows) {
			return inputsErr
		}
		activeKeys := []string{}
		current := map[string]sqlc.ListHostedReportWorkSummariesRow{}
		latestIssued := map[string]int64{}
		var afterID int64
		for {
			reports, err := q.ListHostedReportWorkSummaries(ctx, sqlc.ListHostedReportWorkSummariesParams{OrganizationID: account.OrganizationID, AccountID: account.ID, AfterID: afterID})
			if err != nil {
				return err
			}
			for _, row := range reports {
				if row.Status == "issued" {
					latestIssued[row.SeriesID] = max(latestIssued[row.SeriesID], row.Number)
				}
				key := periodWorkKey(reporting.Period{Kind: row.Kind, Start: row.PeriodStart, End: row.PeriodEnd})
				if row.Kind == "ad_hoc" || row.CorrectionOf.Valid {
					key = "report:" + row.ReportID
				}
				old, ok := current[key]
				if !ok || row.CreatedAt.After(old.CreatedAt) || (row.SeriesID == old.SeriesID && row.Number > old.Number) {
					current[key] = row
				}
			}
			if len(reports) < 100 {
				break
			}
			afterID = reports[len(reports)-1].ID
		}
		if row, err := inputs, inputsErr; err == nil {
			var c reporting.Configuration
			if err := json.Unmarshal(row.Configuration, &c); err != nil {
				return fmt.Errorf("decode reporting configuration: %w", err)
			}
			if schedule := c.Schedule; schedule != nil && strings.TrimSpace(c.ContractRef) != "" {
				owner, err := q.GetReportingUser(ctx, sqlc.GetReportingUserParams{OrganizationID: account.OrganizationID, UserID: schedule.OwnerUserID})
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}
				if err == nil {
					loc, _ := time.LoadLocation(c.Timezone)
					for _, cadence := range []struct {
						kind   string
						start  time.Time
						months int
					}{{"monthly", schedule.MonthlyStart, 1}, {"quarterly", schedule.QuarterlyStart, 3}, {"annual", schedule.AnnualStart, 12}} {
						if cadence.start.IsZero() {
							continue
						}
						for start := cadence.start.In(loc); !start.AddDate(0, cadence.months, 0).After(now); start = start.AddDate(0, cadence.months, 0) {
							end := start.AddDate(0, cadence.months, 0)
							p := reporting.Period{Kind: cadence.kind, Start: start, End: end, AsOf: end, Timezone: c.Timezone}
							key := periodWorkKey(p)
							if _, ok := current[key]; ok {
								continue
							}
							covered, err := q.ReportingPeriodHasCoverage(ctx, sqlc.ReportingPeriodHasCoverageParams{OrganizationID: account.OrganizationID, AccountID: account.ID, PeriodStart: start, PeriodEnd: end, StartSeconds: start.Unix(), StartNanos: int64(start.Nanosecond()), EndSeconds: end.Unix(), EndNanos: int64(end.Nanosecond())})
							if err != nil {
								return err
							}
							if !covered {
								continue
							}
							state, err := missingReportState(ctx, q, account, key, p, row, c, schedule.OwnerUserID)
							if err != nil {
								return err
							}
							queueDeadlines(&state, now)
							activeKeys = append(activeKeys, key)
							if err := persistQueue(ctx, q, account, key, owner, row.SiteIds, state, now); err != nil {
								return err
							}
						}
					}
				}
			}
		}
		// An issued correction replaces every earlier version in its series.
		// Their keys still mark the period as reported above, but they carry no
		// further delivery work and are retired below; a correction that is not
		// yet issued leaves the version it corrects active.
		keys := make([]string, 0, len(current))
		for key, row := range current {
			if row.Number < latestIssued[row.SeriesID] {
				continue
			}
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			row := current[key]
			var result reporting.Result
			if err := json.Unmarshal(row.Result, &result); err != nil {
				return fmt.Errorf("decode work summary: %w", err)
			}
			if err := json.Unmarshal(row.Period, &result.Period); err != nil {
				return fmt.Errorf("decode work period: %w", err)
			}
			owner, err := q.HostedReportingUserUUID(ctx, row.OwnerID)
			if err != nil {
				return err
			}
			state := ReportQueueState{Kind: row.Kind, Period: result.Period, OwnerUserID: owner, ReportID: row.ReportID, ReportRevision: row.Revision, ReportStatus: row.Status, Complete: result.Complete, Deadlines: result.Deadlines, DeliveryStatus: "not_issued"}
			if row.Status != "issued" {
				state.Reasons = append(state.Reasons, row.Status)
			} else {
				state.DeliveryStatus = row.DeliveryStatus
				if state.DeliveryStatus != reportDeliveryConfirmed {
					state.Reasons = append(state.Reasons, "delivery_"+state.DeliveryStatus)
				}
			}
			if !state.Complete {
				state.Reasons = append(state.Reasons, "incomplete_inputs")
			}
			state.Reasons = append(state.Reasons, reportInputCauses(result)...)
			queueDeadlines(&state, now)
			if row.Status == "issued" && state.DeliveryStatus == reportDeliveryConfirmed {
				state.Reasons = nil
			}
			activeKeys = append(activeKeys, key)
			if err := persistQueue(ctx, q, account, key, row.OwnerID, row.SiteIds, state, now); err != nil {
				return err
			}
		}
		// The queue tracks every recorded request, so read the paged history to its end.
		const requestPage = 500
		requestArgs := sqlc.ListHostedReportRequestsParams{OrganizationID: account.OrganizationID, AccountID: account.ID, PageLimit: requestPage}
		var requests []sqlc.ListHostedReportRequestsRow
		for {
			page, err := q.ListHostedReportRequests(ctx, requestArgs)
			if err != nil {
				return err
			}
			requests = append(requests, page...)
			if len(page) < requestPage {
				break
			}
			last := page[len(page)-1]
			requestArgs.BeforeID, requestArgs.BeforeReceivedAt = last.ID, last.ReceivedAt
		}
		for _, row := range requests {
			owner := row.OwnerUserID
			request := ReportRequest{RequestID: row.RequestID, ReceivedAt: row.ReceivedAt, SourceRef: row.SourceRef, Requester: row.Requester, Scope: row.Scope, OwnerUserID: owner, TargetAt: row.TargetAt, Limitations: row.Limitations, CommunicationRef: row.CommunicationRef, DeliveryRef: row.DeliveryRef, FulfilledAt: requestFulfilledAt(row.FulfilledAt), FulfillmentRef: row.FulfillmentRef}
			state := ReportQueueState{Kind: "ad_hoc", OwnerUserID: owner, Request: &request, DeliveryStatus: "unconfirmed", Reasons: []string{"ad_hoc_request_pending"}, Deadlines: reporting.Deadlines{Contract: reporting.Target{Status: reporting.Unavailable, Basis: "reasonable_efforts_request_target", ReasonableEfforts: true}, Internal: reporting.Target{Date: row.TargetAt.Format("2006-01-02"), Status: reporting.Provisional, Basis: "reasonable_efforts_request_target", ReasonableEfforts: true}}}
			if now.After(row.TargetAt) {
				state.Reasons = append(state.Reasons, "request_target_overdue")
			}
			if !row.FulfilledAt.IsZero() && row.FulfillmentRef != "" {
				state.DeliveryStatus = reportDeliveryConfirmed
				state.Reasons = nil
				state.Complete = true
			}
			sites := []int64{}
			activeKeys = append(activeKeys, "request:"+row.RequestID)
			if err := persistQueue(ctx, q, account, "request:"+row.RequestID, row.OwnerID, sites, state, now); err != nil {
				return err
			}
		}
		retired, err := q.RetireHostedReportQueue(ctx, sqlc.RetireHostedReportQueueParams{OrganizationID: account.OrganizationID, AccountID: account.ID, ActiveWorkKeys: activeKeys, NowAt: now})
		if err != nil {
			return err
		}
		for _, row := range retired {
			if err := q.AddHostedReportQueueEvent(ctx, sqlc.AddHostedReportQueueEventParams{QueueInternalID: row.ID, Generation: row.Generation, Kind: "changed", Reason: "reporting queue work superseded", CreatedAt: now}); err != nil {
				return err
			}
		}
		return nil
	})
}
func reportInputCauses(result reporting.Result) []string {
	causes := []string{}
	for _, metric := range result.Metrics {
		causes = append(causes, metric.Causes...)
	}
	return causes
}
func queueDeadlines(state *ReportQueueState, now time.Time) {
	if state.Deadlines.Contract.Status != reporting.Authoritative || state.Deadlines.Contract.ReasonableEfforts {
		state.Deadlines.Contract.Date = ""
		state.Deadlines.Contract.Status = reporting.Unavailable
	}
	if state.DeliveryStatus == reportDeliveryConfirmed {
		return
	}
	today := now.In(mustQueueLocation(state.Period.Timezone)).Format("2006-01-02")
	for _, target := range []struct {
		value reporting.Target
		name  string
	}{{state.Deadlines.Contract, "contract"}, {state.Deadlines.Internal, "internal_target"}} {
		if target.value.Date == "" {
			continue
		}
		if today > target.value.Date {
			state.Reasons = append(state.Reasons, target.name+"_overdue")
		} else if today == target.value.Date {
			state.Reasons = append(state.Reasons, target.name+"_due")
		}
	}
}
func mustQueueLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}
func persistQueue(ctx context.Context, q sqlc.Querier, account Account, key string, owner int64, sites []int64, state ReportQueueState, now time.Time) error {
	slices.Sort(state.Reasons)
	state.Reasons = slices.Compact(state.Reasons)
	if sites == nil {
		sites = []int64{}
	}
	state.ActionPath = "/accounts/reports?account=" + account.AccountID + "&queue="
	raw, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode reporting queue state: %w", err)
	}
	digest := sha256.Sum256(append(raw, []byte(fmt.Sprint(owner, sites))...))
	status := "pending"
	if len(state.Reasons) == 0 {
		status = "quiet"
	}
	row, err := q.UpsertHostedReportQueue(ctx, sqlc.UpsertHostedReportQueueParams{QueueID: uuid.NewString(), OrganizationID: account.OrganizationID, AccountID: account.ID, WorkKey: key, OwnerID: owner, SiteIds: sites, Projection: raw, Fingerprint: hex.EncodeToString(digest[:]), NotificationStatus: status, NowAt: now})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = q.AddHostedReportQueueEvent(ctx, sqlc.AddHostedReportQueueEventParams{QueueInternalID: row.ID, Generation: row.Generation, Kind: "changed", Reason: "reporting queue state changed", CreatedAt: now}); err != nil {
		return err
	}
	alert := queueAlert(row, state)
	if status == "quiet" {
		alert.Status = "resolved"
	}
	labels, err := json.Marshal(alert.Labels)
	if err != nil {
		return fmt.Errorf("encode reporting alert labels: %w", err)
	}
	annotations, err := json.Marshal(alert.Annotations)
	if err != nil {
		return fmt.Errorf("encode reporting alert annotations: %w", err)
	}
	return q.InsertNotificationHistory(ctx, sqlc.InsertNotificationHistoryParams{AlertName: alert.Labels["alertname"], Status: alert.Status, Severity: "warning", RuleGroup: "hosted-reporting", Fingerprint: row.QueueID, OrganizationID: sql.NullInt64{Int64: account.OrganizationID, Valid: true}, Summary: alert.Annotations["summary"], StartsAt: sql.NullTime{Time: now, Valid: true}, Labels: labels, Annotations: annotations})
}

type ReportQueuePage struct {
	Items         []ReportQueueItem
	NextPageToken string
}
type reportQueueCursor struct {
	OrganizationID int64 `json:"organizationId"`
	ActorID        int64 `json:"actorId"`
	BeforeID       int64 `json:"beforeId"`
}

func (s *Service) HostReportQueue(ctx context.Context, actorID, orgID int64, pageSize int32, pageToken string) (ReportQueuePage, error) {
	empty := ReportQueuePage{Items: []ReportQueueItem{}}
	if pageSize < 0 || pageSize > 100 || len(pageToken) > 512 {
		return empty, fleeterror.NewInvalidArgumentError("invalid reporting queue page")
	}
	if pageSize == 0 {
		pageSize = 50
	}
	cursor := reportQueueCursor{OrganizationID: orgID, ActorID: actorID}
	if pageToken != "" {
		raw, err := base64.RawURLEncoding.DecodeString(pageToken)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.OrganizationID != orgID || cursor.ActorID != actorID || cursor.BeforeID <= 0 {
			return empty, fleeterror.NewInvalidArgumentError("invalid reporting queue cursor")
		}
	}
	accounts, err := s.HostReportingAccounts(ctx, actorID, orgID)
	if err != nil {
		return empty, err
	}
	accountIDs := []string{}
	for _, account := range accounts {
		if account.CanCreate {
			accountIDs = append(accountIDs, account.AccountID)
		}
	}
	if len(accountIDs) == 0 {
		return empty, nil
	}
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (ReportQueuePage, error) {
		rows, err := q.ListHostedReportQueue(ctx, sqlc.ListHostedReportQueueParams{OrganizationID: orgID, AccountIds: accountIDs, BeforeID: cursor.BeforeID, PageLimit: pageSize + 1})
		if err != nil {
			return empty, err
		}
		out := ReportQueuePage{Items: []ReportQueueItem{}}
		if len(rows) > int(pageSize) {
			rows = rows[:pageSize]
			cursor.BeforeID = rows[len(rows)-1].ID
			raw, _ := json.Marshal(cursor)
			out.NextPageToken = base64.RawURLEncoding.EncodeToString(raw)
		}
		type accountAccess struct {
			admission  reportingAccess
			ownerNames map[int64]string
			denied     bool
		}
		access := map[int64]accountAccess{}
		for _, row := range rows {
			cached, ok := access[row.AccountID]
			if !ok {
				account, err := q.GetReportingJobAccount(ctx, sqlc.GetReportingJobAccountParams{OrganizationID: orgID, ID: row.AccountID})
				if err != nil {
					return empty, err
				}
				a, err := reportingHostReadAdmission(ctx, q, actorID, orgID, account.AccountID, "read")
				if fleeterror.IsForbiddenError(err) {
					access[row.AccountID] = accountAccess{denied: true}
					continue
				}
				if err != nil {
					return empty, err
				}
				authorities, err := q.ListReportingAuthorities(ctx, sqlc.ListReportingAuthoritiesParams{OrganizationID: orgID, AccountID: a.account.ID})
				if err != nil {
					return empty, err
				}
				cached = accountAccess{admission: a, ownerNames: map[int64]string{}}
				for _, authority := range authorities {
					cached.ownerNames[authority.UserID] = authority.Username
				}
				access[row.AccountID] = cached
			}
			if cached.denied {
				continue
			}
			a := cached.admission
			if !a.sitesAllowed(row.SiteIds) {
				continue
			}
			var state ReportQueueState
			if err := json.Unmarshal(row.Projection, &state); err != nil {
				return empty, fmt.Errorf("decode reporting queue state: %w", err)
			}
			if state.Request != nil && !a.accountWideRequestsAllowed() {
				continue
			}
			item := ReportQueueItem{OwnerUsername: cached.ownerNames[row.OwnerID], ReportQueueState: state, QueueID: row.QueueID, AccountID: a.account.AccountID, AccountName: a.account.Name, Generation: row.Generation, Attempts: row.Attempts, NotificationStatus: row.NotificationStatus, CanAcknowledge: row.NotificationStatus != "acknowledged", CanRetry: slices.Contains([]string{"failed", "acknowledged", "sent", "unrouted", "exhausted", "blocked"}, row.NotificationStatus)}
			if state.ReportID != "" {
				reports, err := q.ListHostedReportSummaries(ctx, sqlc.ListHostedReportSummariesParams{OrganizationID: orgID, AccountID: a.account.ID, Host: true, ReportID: state.ReportID, PageLimit: 1})
				if err != nil {
					return empty, err
				}
				if len(reports) == 0 {
					return empty, sql.ErrNoRows
				}
				report := reports[0]
				if !a.sitesAllowed(report.SiteIds) {
					continue
				}
				record, err := reportSummaryRecord(a.account, report)
				if err != nil {
					return empty, err
				}
				item.Report = &record
			}
			events, err := q.ListHostedReportQueueEvents(ctx, row.ID)
			if err != nil {
				return empty, err
			}
			for _, event := range events {
				item.Events = append(item.Events, ReportQueueEvent{Generation: event.Generation, Kind: event.Kind, ActorUserID: event.ActorUserID, Reason: event.Reason, CreatedAt: event.CreatedAt})
			}
			out.Items = append(out.Items, item)
		}
		return out, nil
	})
}
func (s *Service) AcknowledgeReportQueue(ctx context.Context, actorID, orgID int64, accountID, queueID string, generation int64, reason string) error {
	return s.changeQueueNotification(ctx, actorID, orgID, accountID, queueID, generation, reason, false)
}
func (s *Service) RetryReportQueueNotification(ctx context.Context, actorID, orgID int64, accountID, queueID string, generation int64, reason string) error {
	return s.changeQueueNotification(ctx, actorID, orgID, accountID, queueID, generation, reason, true)
}
func (s *Service) changeQueueNotification(ctx context.Context, actorID, orgID int64, accountID, queueID string, generation int64, reason string, retry bool) error {
	if generation <= 0 || strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return fleeterror.NewInvalidArgumentError("queue generation and action reason required")
	}
	return db.WithTransactionTimeoutNoResult(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) error {
		a, err := reportingHostAdmission(ctx, q, actorID, orgID, accountID, "read")
		if err != nil {
			return err
		}
		row, err := q.GetHostedReportQueue(ctx, sqlc.GetHostedReportQueueParams{OrganizationID: orgID, AccountID: a.account.ID, QueueID: queueID})
		if err != nil {
			return accessError(err)
		}
		if !a.sitesAllowed(row.SiteIds) {
			return denied()
		}
		var state ReportQueueState
		if err = json.Unmarshal(row.Projection, &state); err != nil {
			return fmt.Errorf("decode reporting queue action state: %w", err)
		}
		if state.Request != nil && !a.accountWideRequestsAllowed() {
			return denied()
		}
		if row.Generation != generation {
			return reportingConflict()
		}
		if row.NotificationStatus == "quiet" {
			return reportingConflict()
		}
		now := time.Now().UTC()
		kind := "acknowledged"
		var changed int64
		if retry {
			kind = "retry"
			changed, err = q.RetryHostedReportQueueNotification(ctx, sqlc.RetryHostedReportQueueNotificationParams{ID: row.ID, Generation: generation, RetryAt: now})
		} else {
			changed, err = q.AcknowledgeHostedReportQueue(ctx, sqlc.AcknowledgeHostedReportQueueParams{ID: row.ID, Generation: generation, UpdatedAt: now})
		}
		if err != nil {
			return err
		}
		if changed != 1 {
			return reportingConflict()
		}
		if err = q.AddHostedReportQueueEvent(ctx, sqlc.AddHostedReportQueueEventParams{QueueInternalID: row.ID, Generation: generation, Kind: kind, ActorID: sql.NullInt64{Int64: actorID, Valid: true}, Reason: strings.TrimSpace(reason), CreatedAt: now}); err != nil {
			return err
		}
		return audit(ctx, q, a.actor, a.account, "report_queue_"+kind, map[string]any{"queue_id": queueID, "generation": generation, "reason": strings.TrimSpace(reason)})
	})
}

func missingReportState(ctx context.Context, q sqlc.Querier, account Account, key string, p reporting.Period, input sqlc.HostedReportingInput, config reporting.Configuration, owner string) (ReportQueueState, error) {
	state := ReportQueueState{}
	raw, err := q.GetHostedReportQueueWork(ctx, sqlc.GetHostedReportQueueWorkParams{OrganizationID: account.OrganizationID, AccountID: account.ID, WorkKey: key})
	if err == nil {
		if err = json.Unmarshal(raw, &state); err != nil {
			return state, fmt.Errorf("decode cached reporting period: %w", err)
		}
		if state.InputRevision == input.Revision && state.ReportID == "" {
			state.Reasons = slices.DeleteFunc(state.Reasons, func(reason string) bool {
				return strings.HasSuffix(reason, "_due") || strings.HasSuffix(reason, "_overdue")
			})
			return state, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return state, fmt.Errorf("read cached reporting period: %w", err)
	}
	selected, err := loadReportingContext(ctx, q, account.OrganizationID, account.ID, config, p)
	if err != nil {
		return state, err
	}
	result, err := reporting.CalculateStoredContext(ctx, p, selected.Inputs)
	if err != nil {
		return state, err
	}
	state = ReportQueueState{Kind: p.Kind, Period: p, InputRevision: input.Revision, OwnerUserID: owner, Complete: result.Complete, Deadlines: result.Deadlines, DeliveryStatus: "not_issued", Reasons: []string{"report_missing"}}
	if !result.Complete {
		state.Reasons = append(state.Reasons, "incomplete_inputs")
	}
	state.Reasons = append(state.Reasons, reportInputCauses(result)...)
	return state, nil
}
