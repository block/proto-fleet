package evidence

import (
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/rollout"
)

const (
	defaultTickInterval  = 5 * time.Second
	defaultBatchSize     = 20
	postWindow           = 30 * time.Minute
	staleAfter           = 20 * time.Second
	PolicyBucketDuration = 10 * time.Second
)

type Config struct {
	TickInterval time.Duration `help:"Interval between rollout evidence evaluation passes." default:"5s" env:"TICK_INTERVAL"`
	BatchSize    int32         `help:"Maximum completed rollout batches evaluated per pass." default:"20" env:"BATCH_SIZE"`
}

func (c Config) withDefaults() Config {
	if c.TickInterval <= 0 {
		c.TickInterval = defaultTickInterval
	}
	if c.BatchSize <= 0 {
		c.BatchSize = defaultBatchSize
	}
	return c
}

type Candidate struct {
	RolloutID                          uuid.UUID
	BatchID                            int64
	OrgID                              int64
	CompletedAt                        time.Time
	PolicyEnabled                      bool
	MaxDropBasisPoints                 int32
	HealthyDurationSeconds             int32
	RolloutState                       rollout.State
	RolloutRevision                    int64
	RolloutCreatedByUserID             int64
	IsCurrentReviewBatch               bool
	HasPendingBatch                    bool
	Status                             rollout.EvidenceStatus
	HealthySince                       *time.Time
	LastPolicyBucketBoundary           *time.Time
	LatestPolicyBucketHashrateHS       *float64
	LatestPolicyBucketDeltaBasisPoints *int32
	EvaluatedAt                        *time.Time
	ErrorMessage                       *string
	AutoControlStatus                  *rollout.ControlStatus
	AutoControlExpectedRevision        *int64
	AutoControlResultingRevision       *int64
}

type MemberEvidence struct {
	MemberID           int64
	BaselineHashrateHS *float64
	PostHashrateHS     *float64
	PostObservedAt     *time.Time
}

type BucketMember struct {
	MemberID      int64
	AvgHashrateHS float64
	ObservedAt    time.Time
}

type PolicyBucket struct {
	Boundary time.Time
	Members  []BucketMember
}

type Snapshot struct {
	Members       []MemberEvidence
	PolicyBuckets []PolicyBucket
}

type Summary struct {
	RolloutID                          uuid.UUID
	BatchID                            int64
	OrgID                              int64
	Status                             rollout.EvidenceStatus
	TotalCount                         int64
	PairedCount                        int64
	CumulativeBaselineHashrateHS       *float64
	CumulativeCurrentHashrateHS        *float64
	CumulativeDeltaBasisPoints         *int32
	LatestPolicyBucketHashrateHS       *float64
	LatestPolicyBucketDeltaBasisPoints *int32
	HealthySince                       *time.Time
	LastPolicyBucketBoundary           *time.Time
	ExpectedEvaluatedAt                *time.Time
	ExpectedLastPolicyBucketBoundary   *time.Time
	EvaluatedAt                        time.Time
	PostWindowFinalized                bool
	PostWindowFinalizedAt              *time.Time
	ErrorMessage                       *string
}
