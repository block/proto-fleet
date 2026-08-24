package sqlstores

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/evidence"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

type SQLRolloutEvidenceStore struct {
	conn *sql.DB
}

var _ evidence.Store = (*SQLRolloutEvidenceStore)(nil)

func NewSQLRolloutEvidenceStore(conn *sql.DB) *SQLRolloutEvidenceStore {
	return &SQLRolloutEvidenceStore{conn: conn}
}

func (s *SQLRolloutEvidenceStore) ListCandidates(
	ctx context.Context,
	limit int32,
) ([]evidence.Candidate, error) {
	rows, err := db.WithTransaction(
		ctx,
		s.conn,
		func(q sqlc.Querier) ([]sqlc.ListFirmwareRolloutEvidenceCandidatesRow, error) {
			return q.ListFirmwareRolloutEvidenceCandidates(ctx, limit)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list firmware rollout evidence candidates: %w", err)
	}

	result := make([]evidence.Candidate, 0, len(rows))
	for _, row := range rows {
		if !row.CompletedAt.Valid {
			continue
		}
		result = append(result, evidence.Candidate{
			RolloutID:                          row.RolloutID,
			BatchID:                            row.BatchID,
			OrgID:                              row.OrgID,
			CompletedAt:                        row.CompletedAt.Time,
			PolicyEnabled:                      row.PolicyEnabled,
			MaxDropBasisPoints:                 row.MaxDropBasisPoints,
			HealthyDurationSeconds:             row.HealthyDurationSeconds,
			RolloutState:                       rollout.State(row.RolloutState),
			RolloutRevision:                    row.RolloutRevision,
			RolloutCreatedByUserID:             row.RolloutCreatedByUserID,
			IsCurrentReviewBatch:               row.IsCurrentReviewBatch,
			HasPendingBatch:                    row.HasPendingBatch,
			Status:                             rollout.EvidenceStatus(row.EvidenceStatus),
			HealthySince:                       timePtr(row.HealthySince),
			LastPolicyBucketBoundary:           timePtr(row.LastPolicyBucketBoundary),
			LatestPolicyBucketHashrateHS:       float64Ptr(row.LatestPolicyBucketHashrateHs),
			LatestPolicyBucketDeltaBasisPoints: nullInt32ToPtr(row.LatestPolicyBucketDeltaBasisPoints),
			EvaluatedAt:                        timePtr(row.EvaluatedAt),
			ErrorMessage:                       stringPtr(row.EvidenceErrorMessage),
			AutoControlStatus:                  controlStatusPtr(row.AutoControlStatus),
			AutoControlExpectedRevision:        nullInt64ToPtr(row.AutoControlExpectedRevision),
			AutoControlResultingRevision:       nullInt64ToPtr(row.AutoControlResultingRevision),
		})
	}
	return result, nil
}

func (s *SQLRolloutEvidenceStore) Refresh(
	ctx context.Context,
	candidate evidence.Candidate,
	windowEnd time.Time,
) (evidence.Snapshot, error) {
	result, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (evidence.Snapshot, error) {
		if _, err := q.CaptureFirmwareRolloutBatchPostEvidence(
			ctx,
			sqlc.CaptureFirmwareRolloutBatchPostEvidenceParams{
				WindowStart: candidate.CompletedAt,
				WindowEnd:   windowEnd,
				RolloutID:   candidate.RolloutID,
				BatchID:     candidate.BatchID,
				OrgID:       candidate.OrgID,
			},
		); err != nil {
			return evidence.Snapshot{}, err
		}

		rows, err := q.ListFirmwareRolloutBatchHashrateEvidence(
			ctx,
			sqlc.ListFirmwareRolloutBatchHashrateEvidenceParams{
				RolloutID: candidate.RolloutID,
				BatchID:   candidate.BatchID,
				OrgID:     candidate.OrgID,
			},
		)
		if err != nil {
			return evidence.Snapshot{}, err
		}
		snapshot := evidence.Snapshot{
			Members: make([]evidence.MemberEvidence, 0, len(rows)),
		}
		for _, row := range rows {
			snapshot.Members = append(snapshot.Members, evidence.MemberEvidence{
				MemberID:           row.MemberID,
				BaselineHashrateHS: float64Ptr(row.BaselineHashrateHs),
				PostHashrateHS:     float64Ptr(row.PostHashrateHs),
				PostObservedAt:     timePtr(row.PostObservedAt),
			})
		}

		if !candidate.PolicyEnabled {
			return snapshot, nil
		}
		bucketCutoff := completedPolicyBucketCutoff(candidate.CompletedAt, windowEnd)
		if !bucketCutoff.After(candidate.CompletedAt) {
			return snapshot, nil
		}
		bucketAfter := candidate.CompletedAt
		if candidate.LastPolicyBucketBoundary != nil {
			bucketAfter = *candidate.LastPolicyBucketBoundary
		}
		bucketRows, err := q.ListCompleteFirmwareRolloutPolicyBuckets(
			ctx,
			sqlc.ListCompleteFirmwareRolloutPolicyBucketsParams{
				RolloutID:    candidate.RolloutID,
				BatchID:      candidate.BatchID,
				OrgID:        candidate.OrgID,
				WindowStart:  candidate.CompletedAt,
				BucketCutoff: bucketCutoff,
				BucketAfter:  bucketAfter,
			},
		)
		if err != nil {
			return evidence.Snapshot{}, err
		}
		if len(bucketRows) == 0 {
			return snapshot, nil
		}

		var bucket *evidence.PolicyBucket
		var bucketIndex int64
		for _, row := range bucketRows {
			if bucket == nil || row.BucketIndex != bucketIndex {
				bucketIndex = row.BucketIndex
				snapshot.PolicyBuckets = append(snapshot.PolicyBuckets, evidence.PolicyBucket{
					Boundary: candidate.CompletedAt.Add(
						time.Duration(bucketIndex+1) * evidence.PolicyBucketDuration,
					),
				})
				bucket = &snapshot.PolicyBuckets[len(snapshot.PolicyBuckets)-1]
			}
			bucket.Members = append(bucket.Members, evidence.BucketMember{
				MemberID:      row.MemberID,
				AvgHashrateHS: row.AvgHashrateHs,
				ObservedAt:    row.ObservedAt,
			})
		}
		return snapshot, nil
	})
	if err != nil {
		return evidence.Snapshot{}, fmt.Errorf("refresh firmware rollout batch evidence: %w", err)
	}
	return result, nil
}

func (s *SQLRolloutEvidenceStore) UpdateSummary(
	ctx context.Context,
	summary evidence.Summary,
) (bool, error) {
	rowsAffected, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (int64, error) {
		rows, updateErr := q.UpdateFirmwareRolloutBatchEvidenceSummary(
			ctx,
			sqlc.UpdateFirmwareRolloutBatchEvidenceSummaryParams{
				EvidenceStatus:                     string(summary.Status),
				EvidenceTotalCount:                 summary.TotalCount,
				EvidencePairedCount:                summary.PairedCount,
				CumulativeBaselineHashrateHs:       nullableFloat64(summary.CumulativeBaselineHashrateHS),
				CumulativeCurrentHashrateHs:        nullableFloat64(summary.CumulativeCurrentHashrateHS),
				CumulativeDeltaBasisPoints:         ptrToNullInt32(summary.CumulativeDeltaBasisPoints),
				LatestPolicyBucketHashrateHs:       nullableFloat64(summary.LatestPolicyBucketHashrateHS),
				LatestPolicyBucketDeltaBasisPoints: ptrToNullInt32(summary.LatestPolicyBucketDeltaBasisPoints),
				HealthySince:                       ptrToNullTime(summary.HealthySince),
				LastPolicyBucketBoundary:           ptrToNullTime(summary.LastPolicyBucketBoundary),
				EvaluatedAt: sql.NullTime{
					Time:  summary.EvaluatedAt,
					Valid: true,
				},
				EvidenceErrorMessage:  ptrToNullString(summary.ErrorMessage),
				PostWindowFinalized:   summary.PostWindowFinalized,
				PostWindowFinalizedAt: ptrToNullTime(summary.PostWindowFinalizedAt),
				ExpectedEvaluatedAt: ptrToNullTime(
					summary.ExpectedEvaluatedAt,
				),
				ExpectedLastPolicyBucketBoundary: ptrToNullTime(
					summary.ExpectedLastPolicyBucketBoundary,
				),
				BatchID:   summary.BatchID,
				RolloutID: summary.RolloutID,
				OrgID:     summary.OrgID,
			},
		)
		if updateErr != nil || rows == 0 {
			return rows, updateErr
		}
		if summary.PostWindowFinalized {
			if _, completeErr := q.CompleteFirmwareRolloutEvidenceRows(
				ctx,
				sqlc.CompleteFirmwareRolloutEvidenceRowsParams{
					BatchID:   summary.BatchID,
					RolloutID: summary.RolloutID,
					OrgID:     summary.OrgID,
				},
			); completeErr != nil {
				return 0, completeErr
			}
			if refreshErr := refreshRolloutGroupForChild(
				ctx,
				q,
				summary.OrgID,
				summary.RolloutID,
			); refreshErr != nil {
				return 0, refreshErr
			}
		}
		return rows, nil
	})
	if err != nil {
		return false, fmt.Errorf("update firmware rollout batch evidence summary: %w", err)
	}
	return rowsAffected == 1, nil
}

func (s *SQLRolloutEvidenceStore) MarkAutomationError(
	ctx context.Context,
	summary evidence.Summary,
) error {
	_, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (int64, error) {
		rows, updateErr := q.MarkFirmwareRolloutBatchAutomationError(
			ctx,
			sqlc.MarkFirmwareRolloutBatchAutomationErrorParams{
				EvidenceErrorMessage: ptrToNullString(summary.ErrorMessage),
				EvaluatedAt: sql.NullTime{
					Time:  summary.EvaluatedAt,
					Valid: true,
				},
				BatchID:   summary.BatchID,
				RolloutID: summary.RolloutID,
				OrgID:     summary.OrgID,
			},
		)
		if updateErr != nil || rows == 0 {
			return rows, updateErr
		}
		return rows, nil
	})
	if err != nil {
		return fmt.Errorf("mark firmware rollout batch automation error: %w", err)
	}
	return nil
}

func refreshRolloutGroupForChild(
	ctx context.Context,
	q sqlc.Querier,
	orgID int64,
	rolloutID uuid.UUID,
) error {
	child, err := q.GetFirmwareRollout(
		ctx,
		sqlc.GetFirmwareRolloutParams{RolloutID: rolloutID, OrgID: orgID},
	)
	if err != nil {
		return err
	}
	if !child.GroupID.Valid {
		return nil
	}
	if child.LaneID.Valid {
		if _, err = releaseRolloutLaneActiveParentIfSettled(
			ctx,
			q,
			child.LaneID.UUID,
			orgID,
		); err != nil {
			return err
		}
	}
	_, err = q.RefreshFirmwareRolloutGroupResult(
		ctx,
		sqlc.RefreshFirmwareRolloutGroupResultParams{
			GroupID: child.GroupID.UUID,
			OrgID:   orgID,
		},
	)
	return err
}

func completedPolicyBucketCutoff(windowStart, windowEnd time.Time) time.Time {
	if !windowEnd.After(windowStart) {
		return windowStart
	}
	return windowStart.Add(windowEnd.Sub(windowStart).Truncate(evidence.PolicyBucketDuration))
}

func nullableFloat64(value *float64) sql.NullFloat64 {
	if value == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *value, Valid: true}
}

func controlStatusPtr(value sql.NullString) *rollout.ControlStatus {
	if !value.Valid {
		return nil
	}
	status := rollout.ControlStatus(value.String)
	return &status
}
