package hostedaccounts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const analysisMaxBytes = 16 * 1024 * 1024
const analysisMaxTickets = 10000
const analysisUnavailable = "Unavailable"
const analysisKindRepairs = "issues_repairs"

func analysisSession(ctx context.Context, actor, org int64, account string) error {
	info, err := session.GetInfo(ctx)
	selected, ok := RequestAccountID(ctx)
	if err != nil || info.UserID != actor || info.OrganizationID != org || info.AuthMethod != session.AuthMethodSession || info.AccountKey || !ok || selected != account {
		return denied()
	}
	return nil
}

func analysisEstate(ctx context.Context, q sqlc.Querier, actor, org int64, account string, generate bool) (miningEstate, error) {
	if err := analysisSession(ctx, actor, org, account); err != nil {
		return miningEstate{}, err
	}
	e, err := loadMiningEstate(ctx, q, actor, org, account, false, miningRead)
	if err != nil {
		return e, err
	}
	if e.account.Depth == 0 {
		return e, denied()
	}
	if generate {
		if err := requireAnalysisAdministrator(ctx, q, actor, org, e.account.ID); err != nil {
			return e, err
		}
	}
	return e, nil
}

// Generation and deletion currently require direct account administration.
// Read authority and ancestor preview never imply this mutation authority.
func requireAnalysisAdministrator(ctx context.Context, q sqlc.Querier, actor, org, account int64) error {
	member, err := q.GetHostedAccountMembership(ctx, sqlc.GetHostedAccountMembershipParams{OrganizationID: org, AccountID: account, UserID: actor})
	if err != nil {
		return accessError(err)
	}
	if member.Role != string(RoleAdministrator) {
		return denied()
	}
	return nil
}

const analysisGenerationCapacity = 4
const analysisGenerationAdmissionTimeout = 2 * time.Second

// Shared by every service instance in this process; waiters hold no DB connection.
var analysisGenerationSlots = make(chan struct{}, analysisGenerationCapacity)

func acquireAnalysisGeneration(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("admit analysis generation: %w", err)
	}
	waiting, cancel := context.WithTimeout(ctx, analysisGenerationAdmissionTimeout)
	defer cancel()
	select {
	case analysisGenerationSlots <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-analysisGenerationSlots
			return nil, fmt.Errorf("admit analysis generation: %w", err)
		}
		return func() { <-analysisGenerationSlots }, nil
	case <-waiting.Done():
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("admit analysis generation: %w", err)
		}
		return nil, fleeterror.NewResourceExhaustedErrorf("analysis generation is busy; try again")
	}
}

func analysisVisible(ctx context.Context, q sqlc.Querier, e miningEstate, actor, org int64, kind string, sites, tickets []int64) (bool, error) {
	for _, site := range sites {
		if !e.dashboardSite(site) {
			return false, nil
		}
	}
	if kind != analysisKindRepairs {
		return true, nil
	}
	allowed, err := analysisTicketsAllowed(ctx, q, e, actor, org)
	if err != nil || !allowed {
		return false, err
	}
	count, err := q.AccountAnalysisTicketsVisible(ctx, sqlc.AccountAnalysisTicketsVisibleParams{OrganizationID: org, AccountID: e.account.ID, TicketIds: tickets})
	return count == int64(len(tickets)), err
}

func analysisTicketsAllowed(ctx context.Context, q sqlc.Querier, e miningEstate, actor, org int64) (bool, error) {
	path, err := loadPath(ctx, q, org, e.account.AccountID)
	if err != nil {
		return false, err
	}
	for _, account := range path {
		if !account.TicketingEnabled {
			return false, nil
		}
	}
	if _, err = q.GetHostedAccountMembership(ctx, sqlc.GetHostedAccountMembershipParams{OrganizationID: org, AccountID: e.account.ID, UserID: actor}); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) GenerateAnalysis(ctx context.Context, actor, org int64, request *pb.GenerateAccountAnalysisRequest) (*pb.AccountAnalysis, error) {
	if request == nil || request.Start == nil || request.End == nil || request.Start.CheckValid() != nil || request.End.CheckValid() != nil {
		return nil, fleeterror.NewInvalidArgumentError("valid analysis dates required")
	}
	requestKey, err := uuid.Parse(request.RequestId)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentError("analysis request ID must be a UUID")
	}
	start, end := request.Start.AsTime(), request.End.AsTime()
	loc, err := time.LoadLocation(request.Timezone)
	if err != nil || request.Timezone == "" || request.Timezone == "Local" || !end.After(start) || end.Sub(start) > dashboardHistoryMaxRange || end.After(time.Now().UTC()) || strings.TrimSpace(request.Name) == "" || len(request.Name) > 200 || request.SiteId != nil && *request.SiteId <= 0 || !slices.Contains([]string{"miner_performance", "energy_usage", "issues_repairs"}, request.Kind) {
		return nil, fleeterror.NewInvalidArgumentError("analysis requires a name, supported type, timezone and completed range of at most 366 days")
	}
	normalized := proto.CloneOf(request)
	normalized.RequestId, normalized.Name = "", strings.TrimSpace(request.Name)
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(normalized)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentError("invalid analysis request")
	}
	requestDigest := sha256.Sum256(payload)
	requestDigestText := hex.EncodeToString(requestDigest[:])
	ctx, cancel := context.WithTimeout(ctx, reportingTransactionTimeout)
	defer cancel()
	if err := analysisSession(ctx, actor, org, request.AccountId); err != nil {
		return nil, err
	}
	release, err := acquireAnalysisGeneration(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return db.WithTransactionTimeout(ctx, s.conn, reportingTransactionTimeout, func(q sqlc.Querier) (*pb.AccountAnalysis, error) {
		txCtx := db.WithTxQueries(ctx, q)
		e, err := analysisEstate(txCtx, q, actor, org, request.AccountId, true)
		if err != nil {
			return nil, err
		}
		if e.capabilities&GenerateReports == 0 {
			return nil, denied()
		}
		if request.SiteId != nil && !e.dashboardSite(*request.SiteId) {
			return nil, denied()
		}
		if _, err := q.LockAccountAnalysisLibrary(txCtx, sqlc.LockAccountAnalysisLibraryParams{OrganizationID: org, ID: e.account.ID}); err != nil {
			return nil, err
		}
		deleted, err := q.AccountAnalysisRequestDeleted(txCtx, sqlc.AccountAnalysisRequestDeletedParams{OrganizationID: org, AccountID: e.account.ID, RequestKey: requestKey.String()})
		if err != nil {
			return nil, err
		}
		if deleted {
			return nil, fleeterror.NewFailedPreconditionError("analysis was permanently deleted; use a new request ID")
		}
		existing, err := q.GetAccountAnalysisRequest(txCtx, sqlc.GetAccountAnalysisRequestParams{OrganizationID: org, AccountID: e.account.ID, RequestKey: requestKey.String()})
		if err == nil {
			if existing.RequestDigest != requestDigestText || existing.CreatedBy != actor {
				return nil, fleeterror.NewFailedPreconditionError("analysis request ID was already used for a different request")
			}
			visible, err := analysisVisible(txCtx, q, e, actor, org, existing.Kind, existing.SiteIds, existing.TicketIds)
			if err != nil {
				return nil, err
			}
			if !visible {
				return nil, denied()
			}
			return decodeAnalysis(existing.Snapshot)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		result := &pb.AccountAnalysis{AnalysisId: uuid.NewString(), AccountId: request.AccountId, Name: strings.TrimSpace(request.Name), Kind: request.Kind, Start: request.Start, End: request.End, Timezone: request.Timezone, CreatedAt: timestamppb.Now(), Limitations: []string{"Self-service analysis; not an independently reviewed official report."}}
		sites, tickets := []int64{}, []int64{}
		switch request.Kind {
		case "miner_performance":
			sites, err = s.analysisPerformance(txCtx, q, e, actor, org, request, result, loc)
		case "energy_usage":
			sites, err = analysisEnergy(txCtx, q, org, e, request, result, loc)
		case analysisKindRepairs:
			sites, tickets, err = s.analysisRepairs(txCtx, actor, org, request, result, loc)
		}
		if err != nil {
			return nil, err
		}
		if request.SiteId != nil {
			sites = append(sites, *request.SiteId)
		}
		slices.Sort(sites)
		sites = slices.Compact(sites)
		// Zero is an internal scope marker for unplaced historical assets. A
		// later site restriction must revoke those observations as well.
		result.SiteIds = slices.DeleteFunc(slices.Clone(sites), func(site int64) bool { return site == 0 })
		visible, err := analysisVisible(txCtx, q, e, actor, org, request.Kind, sites, tickets)
		if err != nil {
			return nil, err
		}
		if !visible {
			return nil, denied()
		}
		snapshot, err := protojson.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("encode analysis snapshot: %w", err)
		}
		data, err := analysisCSV(result)
		if err != nil {
			return nil, err
		}
		if len(snapshot) > analysisMaxBytes || len(data) > analysisMaxBytes {
			return nil, fleeterror.NewInvalidArgumentError("analysis is too large; choose a shorter range or one site")
		}
		digest := sha256.Sum256(data)
		err = q.CreateAccountAnalysis(txCtx, sqlc.CreateAccountAnalysisParams{AnalysisID: result.AnalysisId, OrganizationID: org, AccountID: e.account.ID, Kind: result.Kind, Snapshot: snapshot, SiteIds: sites, TicketIds: tickets, CreatedBy: actor, CreatedAt: result.CreatedAt.AsTime(), CsvData: data, Sha256: hex.EncodeToString(digest[:]), RequestKey: requestKey.String(), RequestDigest: requestDigestText})
		return result, err
	})
}

func analysisTable(key, title string, columns ...string) *pb.AnalysisTable {
	return &pb.AnalysisTable{Key: key, Title: title, Columns: columns}
}
func analysisRow(table *pb.AnalysisTable, cells ...string) {
	table.Rows = append(table.Rows, &pb.AnalysisRow{Cells: cells})
}
func analysisNumber(value *float64, divisor float64) string {
	if value == nil {
		return analysisUnavailable
	}
	return strconv.FormatFloat(*value/divisor, 'f', -1, 64)
}
func analysisTime(value time.Time, loc *time.Location) string {
	return value.In(loc).Format(time.RFC3339)
}
func analysisCount(value int64) string { return strconv.FormatInt(value, 10) }
func analysisEnum(value, prefix string) string {
	label := strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(value, prefix)), "_", " ")
	if label == "unspecified" || label == "" {
		return "Not recorded"
	}
	return strings.ToUpper(label[:1]) + label[1:]
}

func (s *Service) analysisPerformance(ctx context.Context, q sqlc.Querier, e miningEstate, actor, org int64, request *pb.GenerateAccountAnalysisRequest, result *pb.AccountAnalysis, loc *time.Location) ([]int64, error) {
	step := int32(3600)
	if request.End.AsTime().Sub(request.Start.AsTime()) > 10*24*time.Hour {
		step = 86400
	}
	history, err := s.Dashboard(ctx, actor, org, &pb.GetAccountDashboardRequest{AccountId: request.AccountId, SiteId: request.SiteId, StartTime: request.Start, EndTime: request.End, BucketSeconds: step})
	if err != nil {
		return nil, err
	}
	result.Limitations = append(result.Limitations, "Only historical allocation-authorized telemetry is included. Missing readings are unavailable, not zero.", "Hashing availability is the percentage of valid hashrate observations above zero; intentional curtailment can lower it. It is not contractual uptime. No period average is inferred from bucket averages.", "Older history uses whole UTC source buckets, including intersecting range edges. No historical container grouping is inferred.")
	if history.HistoryAvailableFrom != nil {
		result.Limitations = append(result.Limitations, "History available from "+analysisTime(history.HistoryAvailableFrom.AsTime(), loc)+"; earlier ownership and readings are unknown.")
	}
	table := analysisTable("performance", "Historical performance", "Bucket start", "Assigned assets", "Miners", "Modules", "Hashrate (TH/s)", "Hashrate reporting", "Power (kW)", "Power reporting", "Efficiency (J/TH)", "Paired reporting", "Temperature (°C)", "Temperature reporting", "Observed hashing availability (%)", "Hashing availability reporting", "Hashing", "Offline", "Unknown", "Partial modules")
	for _, reading := range history.Readings {
		v := reading.GetSummary()
		if v == nil {
			continue
		}
		analysisRow(table, analysisTime(reading.Time.AsTime(), loc), analysisCount(v.AssetCount), analysisCount(v.MinerCount), analysisCount(v.ModuleCount), analysisNumber(v.HashrateHs, 1e12), analysisCount(v.HashrateReportingCount), analysisNumber(v.PowerW, 1000), analysisCount(v.PowerReportingCount), analysisNumber(v.EfficiencyJTh, 1), analysisCount(v.PairedReportingCount), analysisNumber(v.TemperatureC, 1), analysisCount(v.TemperatureReportingCount), analysisNumber(v.UptimePercent, 1), analysisCount(v.UptimeReportingCount), analysisCount(v.HashingCount), analysisCount(v.OfflineCount), analysisCount(v.UnknownCount), analysisCount(v.PartialModuleCount))
	}
	result.Tables = append(result.Tables, table)
	start := request.Start.AsTime()
	if len(history.Readings) > 0 {
		start = history.Readings[0].Time.AsTime()
	}
	intervals, err := q.ListHostedAccountHistoryIntervals(ctx, sqlc.ListHostedAccountHistoryIntervalsParams{OrganizationID: org, AccountID: e.account.ID, StartTime: start, EndTime: request.End.AsTime()})
	if err != nil {
		return nil, err
	}
	sites := []int64{}
	for _, interval := range intervals {
		if (request.SiteId == nil || interval.SiteID == *request.SiteId) && e.dashboardSite(interval.SiteID) {
			sites = append(sites, interval.SiteID)
		}
	}
	return sites, nil
}

func analysisEnergy(ctx context.Context, q sqlc.Querier, org int64, e miningEstate, request *pb.GenerateAccountAnalysisRequest, result *pb.AccountAnalysis, loc *time.Location) ([]int64, error) {
	result.Limitations = append(result.Limitations, "Energy uses recorded meter intervals and explicit allocation shares. Sampled equipment power is never converted to metered energy.", "Intervals crossing the selected range are excluded, without prorating. Missing, reset, estimated or incomplete evidence remains qualified.")
	summary := analysisTable("energy_summary", "Energy summary", "Metric", "Value", "Unit", "Data quality")
	detail := analysisTable("meter_intervals", "Recorded meter intervals", "Start", "End", "Site ID", "Container ID", "Meter", "Attributed energy (kWh)", "Quality", "Range eligibility")
	result.Tables = append(result.Tables, summary, detail)
	period := reporting.Period{Kind: "ad_hoc", Start: request.Start.AsTime(), End: request.End.AsTime(), AsOf: result.CreatedAt.AsTime(), Timezone: request.Timezone}
	inputs, err := loadAnalysisEnergyContext(ctx, q, org, e, request.GetSiteId(), period)
	if errors.Is(err, sql.ErrNoRows) {
		analysisRow(summary, "Metered energy", analysisUnavailable, "kWh", "No recorded meter inputs")
		return []int64{}, nil
	}
	if err != nil {
		return nil, err
	}
	metric, err := reporting.MeteredEnergy(ctx, period, inputs)
	if err != nil {
		return nil, err
	}
	value := metric.Value
	if metric.Status == reporting.Unavailable {
		value = analysisUnavailable
	}
	analysisRow(summary, "Metered energy", value, "kWh", string(metric.Status))
	for _, cause := range metric.Causes {
		message := strings.ReplaceAll(cause, "_", " ")
		if cause == "missing_G3" {
			message = "metering verification is incomplete"
		} else if cause == "missing_G2" {
			message = "source certification approval is incomplete"
		}
		result.Limitations = append(result.Limitations, "Energy evidence: "+message)
	}
	sites := []int64{}
	start, end := request.Start.AsTime(), request.End.AsTime()
	meterLabels := map[string]string{}
	for _, c := range inputs.Coverage {
		if c.Start.Before(end) && (c.End.IsZero() || c.End.After(start)) {
			sites = append(sites, c.SiteID)
		}
	}
	intervals, err := reporting.SupportedMeterIntervals(ctx, period, inputs)
	if err != nil {
		return nil, err
	}
	for _, m := range intervals {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("build energy analysis: %w", err)
		}
		reading, err := reporting.AttributedMeterEnergy(m)
		if err != nil {
			return nil, err
		}
		if reading == "" {
			reading = analysisUnavailable
		}
		label, exists := meterLabels[m.MeterID]
		if !exists {
			label = fmt.Sprintf("Meter %d", len(meterLabels)+1)
			meterLabels[m.MeterID] = label
		}
		analysisRow(detail, analysisTime(m.Start, loc), analysisTime(m.End, loc), analysisCount(m.SiteID), analysisCount(m.ContainerID), label, reading, m.Quality, "Whole interval; allocation evidenced")
		sites = append(sites, m.SiteID)
	}
	return sites, nil
}

func (s *Service) analysisRepairs(ctx context.Context, actor, org int64, request *pb.GenerateAccountAnalysisRequest, result *pb.AccountAnalysis, loc *time.Location) ([]int64, []int64, error) {
	result.Limitations = append(result.Limitations, "Only tickets shared with this account and authorized target details are included. Status is current at generation, not historical status.", "Tickets are selected by creation/completion timestamps: opened, completed, or spanning the range. Reopened tickets have no retained prior completion timestamp; historical backlog and repair-stage durations are not inferred.")
	table := analysisTable("repairs", "Repair activity", "Ticket", "Current status", "Opened", "Completed", "Component", "Description", "Resolution", "Resolution note", "Urgent", "Warranty", "Authorized assets", "Recorded container IDs", "Range relevance")
	result.Tables = append(result.Tables, table)
	sites, tickets := []int64{}, []int64{}
	start, end := request.Start.AsTime(), request.End.AsTime()
	var before int64
	var cellBytes int
	for scanned := 0; ; {
		page, err := s.ListAccountTickets(ctx, actor, org, &pb.ListAccountTicketsRequest{AccountId: request.AccountId, BeforeId: before})
		if err != nil {
			return nil, nil, err
		}
		scanned += len(page.Tickets)
		if scanned > analysisMaxTickets {
			return nil, nil, fleeterror.NewInvalidArgumentError("too many shared tickets for synchronous analysis")
		}
		for _, ticket := range page.Tickets {
			created := ticket.CreatedAt.AsTime()
			if !created.Before(end) || (ticket.CompletedAt != nil && ticket.CompletedAt.AsTime().Before(start)) {
				continue
			}
			labels, containers := []string{}, []string{}
			rowSites := []int64{}
			matched := request.SiteId == nil
			for _, target := range ticket.Targets {
				if target.OutsidePolicy {
					continue
				}
				if request.SiteId != nil && target.GetSiteId() != *request.SiteId {
					continue
				}
				matched = true
				labels = append(labels, target.Label)
				rowSites = append(rowSites, target.GetSiteId())
				if target.GetContainerId() > 0 {
					containers = append(containers, analysisCount(target.GetContainerId()))
				}
			}
			if !matched {
				continue
			}
			completed := ""
			if ticket.CompletedAt != nil {
				completed = analysisTime(ticket.CompletedAt.AsTime(), loc)
			}
			relevance := "Spans range"
			if !created.Before(start) {
				relevance = "Opened in range"
			}
			if ticket.CompletedAt != nil && !ticket.CompletedAt.AsTime().Before(start) && ticket.CompletedAt.AsTime().Before(end) {
				relevance += "; completed in range"
			}
			urgent := "No"
			if ticket.Urgent {
				urgent = "Yes"
			}
			analysisRow(table, ticket.TicketNumber, analysisEnum(ticket.Status.String(), "TICKET_STATUS_"), analysisTime(created, loc), completed, ticket.Component, ticket.Description, analysisEnum(ticket.Resolution.String(), "TICKET_RESOLUTION_"), ticket.ResolutionNote, urgent, analysisEnum(ticket.WarrantyStatus.String(), "WARRANTY_STATUS_"), strings.Join(labels, "; "), strings.Join(containers, "; "), relevance)
			for _, cell := range table.Rows[len(table.Rows)-1].Cells {
				cellBytes += len(cell)
			}
			if cellBytes > analysisMaxBytes/2 {
				return nil, nil, fleeterror.NewInvalidArgumentError("analysis is too large; choose a shorter range or one site")
			}
			sites = append(sites, rowSites...)
			tickets = append(tickets, ticket.Id)
		}
		if page.NextBeforeId == 0 {
			break
		}
		before = page.NextBeforeId
	}
	return sites, tickets, nil
}

func decodeAnalysis(snapshot []byte) (*pb.AccountAnalysis, error) {
	result := &pb.AccountAnalysis{}
	if err := protojson.Unmarshal(snapshot, result); err != nil {
		return nil, fmt.Errorf("decode analysis snapshot: %w", err)
	}
	return result, nil
}

type analysisCursor struct {
	Account  string    `json:"account"`
	Search   string    `json:"search"`
	Kind     string    `json:"kind"`
	BeforeID int64     `json:"beforeId"`
	BeforeAt time.Time `json:"beforeAt"`
}

func (s *Service) AccountAnalyses(ctx context.Context, actor, org int64, request *pb.ListAccountAnalysesRequest) (*pb.ListAccountAnalysesResponse, error) {
	if request == nil || request.PageSize < 0 || request.PageSize > 100 || len(request.PageToken) > 2048 || len(request.Search) > 200 || !slices.Contains([]string{"", "miner_performance", "energy_usage", "issues_repairs"}, request.Kind) {
		return nil, fleeterror.NewInvalidArgumentError("invalid analysis library filters")
	}
	pageSize := request.PageSize
	if pageSize == 0 {
		pageSize = 50
	}
	cursor := analysisCursor{Account: request.AccountId, Search: request.Search, Kind: request.Kind}
	if request.PageToken != "" {
		encoded, err := base64.RawURLEncoding.DecodeString(request.PageToken)
		if err != nil || json.Unmarshal(encoded, &cursor) != nil || cursor.Account != request.AccountId || cursor.Search != request.Search || cursor.Kind != request.Kind || cursor.BeforeID <= 0 || cursor.BeforeAt.IsZero() {
			return nil, fleeterror.NewInvalidArgumentError("invalid analysis page token")
		}
	}
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (*pb.ListAccountAnalysesResponse, error) {
		e, err := analysisEstate(ctx, q, actor, org, request.AccountId, false)
		if err != nil {
			return nil, err
		}
		rows, err := q.ListAccountAnalyses(ctx, sqlc.ListAccountAnalysesParams{OrganizationID: org, AccountID: e.account.ID, BeforeID: cursor.BeforeID, BeforeAt: cursor.BeforeAt, Kind: request.Kind, Search: request.Search, PageLimit: pageSize + 1})
		if err != nil {
			return nil, err
		}
		out := &pb.ListAccountAnalysesResponse{}
		if len(rows) > int(pageSize) {
			rows = rows[:pageSize]
			last := rows[len(rows)-1]
			cursor.BeforeID, cursor.BeforeAt = last.ID, last.CreatedAt
			encoded, err := json.Marshal(cursor)
			if err != nil {
				return nil, fmt.Errorf("encode analysis page token: %w", err)
			}
			out.NextPageToken = base64.RawURLEncoding.EncodeToString(encoded)
		}
		var ticketsChecked, ticketsAllowed bool
		for _, row := range rows {
			visible := true
			for _, site := range row.SiteIds {
				if !e.dashboardSite(site) {
					visible = false
					break
				}
			}
			if !visible {
				continue
			}
			if row.Kind == analysisKindRepairs {
				if !ticketsChecked {
					ticketsAllowed, err = analysisTicketsAllowed(ctx, q, e, actor, org)
					if err != nil {
						return nil, err
					}
					ticketsChecked = true
				}
				if !ticketsAllowed || !row.TicketsVisible {
					continue
				}
			}
			value, err := decodeAnalysis(row.Snapshot)
			if err != nil {
				return nil, err
			}
			out.Analyses = append(out.Analyses, value)
		}
		return out, nil
	})
}

func (s *Service) AccountAnalysis(ctx context.Context, actor, org int64, account, id string) (*pb.AccountAnalysis, error) {
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (*pb.AccountAnalysis, error) {
		e, err := analysisEstate(ctx, q, actor, org, account, false)
		if err != nil {
			return nil, err
		}
		row, err := q.GetAccountAnalysis(ctx, sqlc.GetAccountAnalysisParams{OrganizationID: org, AccountID: e.account.ID, AnalysisID: id})
		if err != nil {
			return nil, accessError(err)
		}
		visible, err := analysisVisible(ctx, q, e, actor, org, row.Kind, row.SiteIds, row.TicketIds)
		if err != nil {
			return nil, err
		}
		if !visible {
			return nil, denied()
		}
		return decodeAnalysis(row.Snapshot)
	})
}

func (s *Service) DeleteAnalysis(ctx context.Context, actor, org int64, account, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return fleeterror.NewInvalidArgumentError("analysis ID must be a UUID")
	}
	ctx, cancel := context.WithTimeout(ctx, miningTransactionTimeout)
	defer cancel()
	return db.WithTransactionTimeoutNoResult(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) error {
		e, err := analysisEstate(ctx, q, actor, org, account, true)
		if err != nil {
			return err
		}
		if _, err := q.LockAccountAnalysisLibrary(ctx, sqlc.LockAccountAnalysisLibraryParams{OrganizationID: org, ID: e.account.ID}); err != nil {
			return err
		}
		row, err := q.GetAccountAnalysis(ctx, sqlc.GetAccountAnalysisParams{OrganizationID: org, AccountID: e.account.ID, AnalysisID: id})
		if errors.Is(err, sql.ErrNoRows) {
			deleted, err := q.AccountAnalysisDeleted(ctx, sqlc.AccountAnalysisDeletedParams{OrganizationID: org, AccountID: e.account.ID, AnalysisID: id})
			if err != nil {
				return err
			}
			if deleted {
				return nil
			}
			return fleeterror.NewNotFoundError("analysis not found")
		}
		if err != nil {
			return err
		}
		visible, err := analysisVisible(ctx, q, e, actor, org, row.Kind, row.SiteIds, row.TicketIds)
		if err != nil {
			return err
		}
		if !visible {
			return denied()
		}
		receipt, err := q.CreateAccountAnalysisDeletion(ctx, sqlc.CreateAccountAnalysisDeletionParams{OrganizationID: org, AccountID: e.account.ID, AnalysisID: id, DeletedBy: actor})
		if err != nil {
			return err
		}
		actorRow, err := q.LockActiveHostedAccountUserForShare(ctx, sqlc.LockActiveHostedAccountUserForShareParams{ID: actor, OrganizationID: org})
		if err != nil {
			return accessError(err)
		}
		metadata, err := json.Marshal(map[string]any{"account_id": account, "analysis_id": id, "request_id": receipt.RequestKey, "request_digest": receipt.RequestDigest, "deleted_at": receipt.DeletedAt})
		if err != nil {
			return fmt.Errorf("encode analysis deletion audit: %w", err)
		}
		if err := q.InsertActivityLog(ctx, sqlc.InsertActivityLogParams{
			EventID: uuid.New(), EventCategory: "auth", EventType: "delete_account_analysis", Description: "delete account analysis", Result: "success", ActorType: "user",
			UserID: sql.NullString{String: actorRow.UserID, Valid: true}, OrganizationID: sql.NullInt64{Int64: org, Valid: true}, ScopeType: sql.NullString{String: "account", Valid: true},
			Metadata: pqtype.NullRawMessage{RawMessage: metadata, Valid: true}, MemberSiteIds: []int64{},
		}); err != nil {
			return err
		}
		changed, err := q.DeleteAccountAnalysis(ctx, sqlc.DeleteAccountAnalysisParams{OrganizationID: org, AccountID: e.account.ID, AnalysisID: id})
		if err != nil {
			return err
		}
		if changed != 1 {
			return fleeterror.NewFailedPreconditionError("analysis changed before deletion")
		}
		return nil
	})
}

func (s *Service) AnalysisArtifactPage(ctx context.Context, actor, org int64, account, id string, sequence int64) (ReportArtifactChunk, error) {
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (ReportArtifactChunk, error) {
		var zero ReportArtifactChunk
		if sequence < 0 || sequence > analysisMaxBytes/reportArtifactChunkSize {
			return zero, fleeterror.NewInvalidArgumentError("invalid analysis chunk")
		}
		e, err := analysisEstate(ctx, q, actor, org, account, false)
		if err != nil {
			return zero, err
		}
		row, err := q.GetAccountAnalysisAccess(ctx, sqlc.GetAccountAnalysisAccessParams{OrganizationID: org, AccountID: e.account.ID, AnalysisID: id})
		if err != nil {
			return zero, accessError(err)
		}
		visible, err := analysisVisible(ctx, q, e, actor, org, row.Kind, row.SiteIds, row.TicketIds)
		if err != nil {
			return zero, err
		}
		if !visible {
			return zero, denied()
		}
		offset := int32(sequence * reportArtifactChunkSize) //nolint:gosec // G115: sequence is bounded above to a 16 MiB artifact.
		page, err := q.GetAccountAnalysisCSVChunk(ctx, sqlc.GetAccountAnalysisCSVChunkParams{OrganizationID: org, AccountID: e.account.ID, AnalysisID: id, ByteOffset: offset})
		if err != nil {
			return zero, err
		}
		chunks := (page.ByteCount + reportArtifactChunkSize - 1) / reportArtifactChunkSize
		if sequence >= chunks {
			return zero, fleeterror.NewInvalidArgumentError("invalid analysis chunk")
		}
		return ReportArtifactChunk{Data: page.Data, Name: "analysis-" + id + ".csv", MediaType: "text/csv; charset=utf-8", SHA256: page.Sha256, Chunks: chunks}, nil
	})
}

func analysisCSV(result *pb.AccountAnalysis) ([]byte, error) {
	var buffer bytes.Buffer
	w := csv.NewWriter(&buffer)
	write := func(cells []string) error {
		safe := slices.Clone(cells)
		for i, value := range safe {
			trimmed := strings.TrimLeft(value, " \t\r\n")
			_, numericErr := reporting.Decimal(value, true)
			if numericErr != nil && len(trimmed) > 0 && strings.ContainsAny(trimmed[:1], "=+-@") {
				safe[i] = "'" + value
			}
		}
		return w.Write(safe)
	}
	for _, row := range [][]string{{"Analysis", result.Name}, {"Type", result.Kind}, {"Start", result.Start.AsTime().Format(time.RFC3339)}, {"End (exclusive)", result.End.AsTime().Format(time.RFC3339)}, {"Timezone", result.Timezone}, {"Generated", result.CreatedAt.AsTime().Format(time.RFC3339)}} {
		if err := write(row); err != nil {
			return nil, err
		}
	}
	for _, limitation := range result.Limitations {
		if err := write([]string{"Limitation", limitation}); err != nil {
			return nil, err
		}
	}
	for _, table := range result.Tables {
		if err := write([]string{table.Title}); err != nil {
			return nil, err
		}
		if err := write(table.Columns); err != nil {
			return nil, err
		}
		for _, row := range table.Rows {
			if err := write(row.Cells); err != nil {
				return nil, err
			}
		}
	}
	w.Flush()
	return buffer.Bytes(), w.Error()
}
