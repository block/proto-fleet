package metrics

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

type Sample struct {
	Time   time.Time
	Metric string
	Labels Labels
	Value  float64
}

type Labels struct {
	OrganizationID string
	SiteID         string
	DeviceID       string
	DeviceGroup    string
	Driver         string
	SensorKind     string
	Kind           string
	Result         string
}

type Store interface {
	InsertSamples(ctx context.Context, samples []Sample) error
	Close() error
}

type sqlStore struct {
	queries *sqlc.Queries
}

func NewSQLStore(db *sql.DB) Store {
	return &sqlStore{queries: sqlc.New(db)}
}

const columnsPerSample = 11

func (s *sqlStore) InsertSamples(ctx context.Context, samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}

	params := InsertParamsFromSamples(samples)
	if err := s.queries.InsertNotificationMetricSamples(ctx, params); err != nil {
		return fmt.Errorf("insert %d metric samples: %w", len(samples), err)
	}
	return nil
}

func InsertParamsFromSamples(samples []Sample) sqlc.InsertNotificationMetricSamplesParams {
	n := len(samples)
	params := sqlc.InsertNotificationMetricSamplesParams{
		Times:           make([]time.Time, n),
		Metrics:         make([]string, n),
		OrganizationIds: make([]string, n),
		SiteIds:         make([]string, n),
		DeviceIds:       make([]string, n),
		DeviceGroups:    make([]string, n),
		Drivers:         make([]string, n),
		SensorKinds:     make([]string, n),
		Kinds:           make([]string, n),
		Results:         make([]string, n),
		Values:          make([]float64, n),
	}
	for i, sample := range samples {
		params.Times[i] = sample.Time
		params.Metrics[i] = sample.Metric
		params.OrganizationIds[i] = sample.Labels.OrganizationID
		params.SiteIds[i] = sample.Labels.SiteID
		params.DeviceIds[i] = sample.Labels.DeviceID
		params.DeviceGroups[i] = sample.Labels.DeviceGroup
		params.Drivers[i] = sample.Labels.Driver
		params.SensorKinds[i] = sample.Labels.SensorKind
		params.Kinds[i] = sample.Labels.Kind
		params.Results[i] = sample.Labels.Result
		params.Values[i] = sample.Value
	}
	return params
}

func (s *sqlStore) Close() error { return nil }

type inMemoryStore struct {
	mu      sync.Mutex
	samples []Sample
	err     error
}

func NewInMemoryStore() *inMemoryStore { //nolint:revive // intentional return of internal type
	return &inMemoryStore{}
}

func (s *inMemoryStore) InsertSamples(_ context.Context, samples []Sample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	// Copy the slice so the caller can re-use its backing array safely.
	copied := make([]Sample, len(samples))
	copy(copied, samples)
	s.samples = append(s.samples, copied...)
	return nil
}

func (s *inMemoryStore) Close() error { return nil }

func (s *inMemoryStore) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *inMemoryStore) Snapshot() []Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Sample, len(s.samples))
	copy(out, s.samples)
	return out
}
