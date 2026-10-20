package hostedaccounts

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"time"

	activitypb "github.com/block/proto-fleet/server/generated/grpc/activity/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"google.golang.org/protobuf/encoding/protojson"
)

type activityCursor struct {
	AccountID string    `json:"account_id"`
	ID        int64     `json:"cursor_id"`
	Time      time.Time `json:"cursor_time"`
}

// Activities holds live account admission through the entire query. The SQL
// projection is shared by rows, totals, filter options, search and CSV export;
// raw organization audit records never cross this boundary.
func (s *Service) Activities(ctx context.Context, actorID, orgID int64, request *pb.ListAccountActivitiesRequest) (*pb.ListAccountActivitiesResponse, error) {
	return s.readActivityPage(ctx, actorID, orgID, request, true)
}

// ActivityExportPage resolves live access for each export chunk without
// aggregating the feed's unused counts and filter options.
func (s *Service) ActivityExportPage(ctx context.Context, actorID, orgID int64, request *pb.ListAccountActivitiesRequest) (*pb.ListAccountActivitiesResponse, error) {
	return s.readActivityPage(ctx, actorID, orgID, request, false)
}

func (s *Service) readActivityPage(ctx context.Context, actorID, orgID int64, request *pb.ListAccountActivitiesRequest, includeSummary bool) (*pb.ListAccountActivitiesResponse, error) {
	if request == nil || request.PageSize < 1 || request.PageSize > 100 {
		return nil, fleeterror.NewInvalidArgumentError("activity page size must be between 1 and 100")
	}
	filter := request.Filter
	if filter == nil {
		filter = &activitypb.ActivityFilter{}
	}
	if len(filter.UserIds) > 0 {
		return nil, fleeterror.NewInvalidArgumentError("account activity does not expose user identities")
	}
	var cursor activityCursor
	if request.PageToken != "" {
		encoded, err := base64.RawURLEncoding.DecodeString(request.PageToken)
		if err != nil || json.Unmarshal(encoded, &cursor) != nil || cursor.AccountID != request.AccountId || cursor.ID <= 0 || cursor.Time.IsZero() {
			return nil, fleeterror.NewInvalidArgumentError("invalid account activity page token")
		}
	}
	params := sqlc.GetHostedAccountActivityPageParams{
		OrganizationID: sql.NullInt64{Int64: orgID, Valid: true}, AccountUuid: request.AccountId,
		Categories: nonNilStrings(filter.EventCategories), EventTypes: nonNilStrings(filter.EventTypes),
		ScopeTypes: nonNilStrings(filter.ScopeTypes), SearchText: filter.SearchText,
		SiteIds: filter.SiteIds, IncludeUnassigned: filter.IncludeUnassigned, PageSize: request.PageSize,
		CursorID: cursor.ID, CursorTime: sql.NullTime{Time: cursor.Time, Valid: request.PageToken != ""},
		IncludeSummary: includeSummary,
	}
	if params.SiteIds == nil {
		params.SiteIds = []int64{}
	}
	if filter.StartTime != nil {
		if err := filter.StartTime.CheckValid(); err != nil {
			return nil, fleeterror.NewInvalidArgumentError("invalid activity start time")
		}
		params.StartTime = sql.NullTime{Time: filter.StartTime.AsTime(), Valid: true}
	}
	if filter.EndTime != nil {
		if err := filter.EndTime.CheckValid(); err != nil {
			return nil, fleeterror.NewInvalidArgumentError("invalid activity end time")
		}
		params.EndTime = sql.NullTime{Time: filter.EndTime.AsTime(), Valid: true}
	}
	ctx, cancel := context.WithTimeout(ctx, miningTransactionTimeout)
	defer cancel()
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (*pb.ListAccountActivitiesResponse, error) {
		e, err := loadMiningEstate(ctx, q, actorID, orgID, request.AccountId, false, miningRead)
		if err != nil {
			return nil, err
		}
		if e.account.Depth == 0 {
			return nil, denied()
		}
		params.AccountInternalID = e.account.ID
		params.PolicyRestricted = e.policy.EffectiveRestricted
		params.PolicySites = e.policy.EffectiveSiteIDs
		if params.PolicySites == nil {
			params.PolicySites = []int64{}
		}
		params.HostAuthority, err = e.physicalReadAuthority()
		if err != nil {
			return nil, err
		}
		payload, err := q.GetHostedAccountActivityPage(ctx, params)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("read account activity: %w", err)
		}
		var envelope struct {
			Page json.RawMessage `json:"page"`
			activityCursor
		}
		if err = json.Unmarshal(payload, &envelope); err != nil {
			return nil, fleeterror.NewInternalErrorf("decode account activity: %w", err)
		}
		response := &pb.ListAccountActivitiesResponse{}
		if err = protojson.Unmarshal(envelope.Page, response); err != nil {
			return nil, fleeterror.NewInternalErrorf("decode account activity page: %w", err)
		}
		authority, err := json.Marshal(struct {
			Restricted bool
			Sites      []int64
			Host       json.RawMessage
		}{params.PolicyRestricted, params.PolicySites, params.HostAuthority})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("encode activity authority: %w", err)
		}
		digest := sha256.Sum256(authority)
		response.AuthorityFingerprint = hex.EncodeToString(digest[:])
		if len(response.Activities) == int(request.PageSize) {
			envelope.AccountID = request.AccountId
			encoded, err := json.Marshal(envelope.activityCursor)
			if err != nil {
				return nil, fleeterror.NewInternalErrorf("encode activity cursor: %w", err)
			}
			response.NextPageToken = base64.RawURLEncoding.EncodeToString(encoded)
		}
		return response, nil
	})
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
