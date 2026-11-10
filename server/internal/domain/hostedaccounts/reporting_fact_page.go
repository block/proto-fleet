package hostedaccounts

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

const MaxReportingFactPageSize = 200
const MaxReportingFactPageBytes = 2 << 20

type ReportingFactPageRecord struct {
	Revision   int64            `json:"revision"`
	Facts      []reporting.Fact `json:"facts"`
	NextCursor string           `json:"nextCursor"`
}
type reportingFactCursor struct {
	OrganizationID int64              `json:"organizationId"`
	AccountID      string             `json:"accountId"`
	Kind           reporting.FactKind `json:"kind"`
	Revision       int64              `json:"revision"`
	AfterID        string             `json:"afterId"`
}

func reportingFactPageCursor(org int64, account string, kind reporting.FactKind, revision int64, id string) (string, error) {
	b, err := json.Marshal(reportingFactCursor{org, account, kind, revision, id})
	if err != nil {
		return "", fmt.Errorf("encode reporting cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Service) ReportingFactPage(ctx context.Context, actorID, orgID int64, accountID string, kind reporting.FactKind, expected int64, cursor string, pageSize int32) (ReportingFactPageRecord, error) {
	var zero ReportingFactPageRecord
	if !kind.Valid() || expected <= 0 || pageSize < 1 || pageSize > MaxReportingFactPageSize || len(cursor) > 20000 {
		return zero, fleeterror.NewInvalidArgumentError("invalid reporting register page")
	}
	after := ""
	if cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil {
			return zero, fleeterror.NewInvalidArgumentError("invalid reporting register cursor")
		}
		var c reportingFactCursor
		if e = json.Unmarshal(b, &c); e != nil || c.OrganizationID != orgID || c.AccountID != accountID || c.Kind != kind || c.Revision != expected || c.AfterID == "" || len(c.AfterID) > 2000 {
			return zero, fleeterror.NewInvalidArgumentError("invalid reporting register cursor")
		}
		canonical, err := json.Marshal(c)
		if err != nil {
			return zero, fmt.Errorf("encode reporting cursor: %w", err)
		}
		if string(canonical) != string(b) {
			return zero, fleeterror.NewInvalidArgumentError("invalid reporting register cursor")
		}
		after = c.AfterID
	}
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (ReportingFactPageRecord, error) {
		a, e := reportingHostReadAdmission(ctx, q, actorID, orgID, accountID, "read")
		if e != nil {
			return zero, e
		}
		row, e := q.LockReportingInputsForShare(ctx, sqlc.LockReportingInputsForShareParams{OrganizationID: orgID, AccountID: a.account.ID})
		if errors.Is(e, sql.ErrNoRows) {
			return zero, fleeterror.NewNotFoundError("reporting inputs not found")
		}
		if e != nil {
			return zero, e
		}
		if !a.hostSitesAllowed(row.SiteIds) {
			return zero, denied()
		}
		if row.Revision != expected {
			return zero, reportingConflict()
		}
		return reportingFactPage(ctx, q, orgID, a.account.ID, accountID, kind, expected, after, pageSize)
	})
}

// reportingFactPage reads within a caller-held shared or exclusive header lock.
// Admission and expected-revision checking belong to that caller.
func reportingFactPage(ctx context.Context, q sqlc.Querier, org, account int64, accountUUID string, kind reporting.FactKind, revision int64, after string, pageSize int32) (ReportingFactPageRecord, error) {
	out := ReportingFactPageRecord{Revision: revision, Facts: []reporting.Fact{}}
	rows, e := q.ListReportingFactPageMetadata(ctx, sqlc.ListReportingFactPageMetadataParams{OrganizationID: org, AccountID: account, Kind: string(kind), AfterID: after, PageLimit: pageSize + 1})
	if e != nil {
		return out, e
	}
	meta := make([]reportingFactMetadata, 0, min(len(rows), int(pageSize)))
	for _, r := range rows[:min(len(rows), int(pageSize))] {
		meta = append(meta, reportingFactMetadata{r.RowID.Int64, kind, r.FactID, r.PayloadBytes.Int64})
	}
	meta = reportingFactPrefix(meta)
	facts, e := reportingFactPayloads(ctx, q, org, account, meta)
	if e != nil {
		return out, e
	}
	envelope, e := json.Marshal(out)
	if e != nil {
		return out, fmt.Errorf("encode reporting page: %w", e)
	}
	encodedFacts := 0
	for i, f := range facts {
		out.Facts = append(out.Facts, f)
		out.NextCursor = ""
		if i+1 < len(rows) {
			out.NextCursor, e = reportingFactPageCursor(org, accountUUID, kind, revision, f.ID)
			if e != nil {
				return out, e
			}
		}
		encodedFact, e := json.Marshal(f)
		if e != nil {
			return out, fmt.Errorf("encode reporting page: %w", e)
		}
		encodedCursor, e := json.Marshal(out.NextCursor)
		if e != nil {
			return out, fmt.Errorf("encode reporting page: %w", e)
		}
		separator := 0
		if len(out.Facts) > 1 {
			separator = 1
		}
		encodedSize := len(envelope) + encodedFacts + separator + len(encodedFact) + len(encodedCursor) - 2
		if encodedSize > MaxReportingFactPageBytes {
			out.Facts = out.Facts[:len(out.Facts)-1]
			if len(out.Facts) == 0 {
				return out, fmt.Errorf("stored reporting fact cannot fit a register page")
			}
			out.NextCursor, e = reportingFactPageCursor(org, accountUUID, kind, revision, out.Facts[len(out.Facts)-1].ID)
			if e != nil {
				return out, e
			}
			break
		}
		encodedFacts += separator + len(encodedFact)
	}
	return out, nil
}
