package fleetinventory_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// This seam checks service forwarding and response mapping, not SQL filtering.
// The real-database module_history tests own recorded-site and row-selection proof.
type moduleHistoryContractStore struct {
	fleetinventory.Store
	scopeErr   error
	allowed    bool
	resolution int32
	calls      []string
	params     []sqlc.ListContainerModuleHistoryParams
	rows       [][]sqlc.ListContainerModuleHistoryRow
}

func (s *moduleHistoryContractStore) GetContainerModuleScope(_ context.Context, p sqlc.GetContainerModuleScopeParams) (sqlc.GetContainerModuleScopeRow, error) {
	s.calls = append(s.calls, "scope")
	// The current site differs from the historical authority passed to History.
	return sqlc.GetContainerModuleScopeRow{OrgID: p.OrgID, SiteID: sql.NullInt64{Int64: 99, Valid: true}}, s.scopeErr
}

func (s *moduleHistoryContractStore) CanReadContainerModuleHistory(_ context.Context, _ sqlc.CanReadContainerModuleHistoryParams) (bool, error) {
	s.calls = append(s.calls, "authority")
	return s.allowed, nil
}

func (s *moduleHistoryContractStore) GetHistoryResolution(_ context.Context, _ sqlc.GetHistoryResolutionParams) (int32, error) {
	s.calls = append(s.calls, "resolution")
	return s.resolution, nil
}

func (s *moduleHistoryContractStore) ListContainerModuleHistory(_ context.Context, p sqlc.ListContainerModuleHistoryParams) ([]sqlc.ListContainerModuleHistoryRow, error) {
	s.calls = append(s.calls, "rows")
	s.params = append(s.params, p)
	rows := s.rows[0]
	s.rows = s.rows[1:]
	return rows, nil
}

func moduleHistoryContractRequest() *pb.ListModuleHistoryRequest {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return &pb.ListModuleHistoryRequest{ModuleId: 91, StartTime: timestamppb.New(start), EndTime: timestamppb.New(start.Add(24 * time.Hour)), PageSize: 1}
}

func TestModuleHistoryContractPreflight(t *testing.T) {
	for _, tc := range []struct {
		name     string
		scopeErr error
		allowed  bool
		calls    []string
	}{
		{"missing module", sql.ErrNoRows, true, []string{"scope"}},
		{"scope failure", errors.New("scope unavailable"), true, []string{"scope"}},
		{"authority denied", nil, false, []string{"scope", "authority"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &moduleHistoryContractStore{scopeErr: tc.scopeErr, allowed: tc.allowed}
			response, err := fleetinventory.NewService(store).History(t.Context(), 7, moduleHistoryContractRequest(), []fleetinventory.Scope{{Sites: []int64{10}}})
			require.Error(t, err)
			require.Nil(t, response)
			require.Equal(t, tc.calls, store.calls)
		})
	}
}

func TestModuleHistoryContractTuplePaginationAndNullableValues(t *testing.T) {
	req := moduleHistoryContractRequest()
	start := req.StartTime.AsTime()
	zero := sql.NullFloat64{Float64: 0, Valid: true}
	first := sqlc.ListContainerModuleHistoryRow{Time: start, CollectedAt: start.Add(time.Second), HashrateHs: zero}
	second := sqlc.ListContainerModuleHistoryRow{Time: start, CollectedAt: start.Add(2 * time.Second), PowerW: zero}
	store := &moduleHistoryContractStore{allowed: true, rows: [][]sqlc.ListContainerModuleHistoryRow{{first, second}, {second}}}
	service := fleetinventory.NewService(store)
	scopes := []fleetinventory.Scope{{Sites: []int64{10}}, {OrgWide: true, Sites: []int64{20}, TelemetryOnly: true}}
	page, err := service.History(t.Context(), 7, req, scopes)
	require.NoError(t, err)
	require.Len(t, page.Samples, 1)
	require.NotEmpty(t, page.NextPageToken)
	require.NotNil(t, page.Samples[0].HashrateHs)
	require.Zero(t, *page.Samples[0].HashrateHs)
	require.Nil(t, page.Samples[0].PowerW)
	require.Equal(t, []string{"scope", "authority", "resolution", "rows"}, store.calls)
	query := store.params[0]
	require.EqualValues(t, 7, query.OrgID)
	require.Equal(t, req.ModuleId, query.ModuleID)
	require.Equal(t, start, query.StartTime)
	require.Equal(t, req.EndTime.AsTime(), query.EndTime)
	require.EqualValues(t, 2, query.PageLimit)
	require.False(t, query.HasCursor)
	require.Zero(t, query.ResolutionSeconds)
	require.JSONEq(t, `[{"org_wide":false,"sites":[10]},{"telemetry_only":true,"org_wide":true,"sites":[20]}]`, string(query.Authority))
	req.PageToken = page.NextPageToken
	page, err = service.History(t.Context(), 7, req, scopes)
	require.NoError(t, err)
	require.Len(t, page.Samples, 1)
	require.Empty(t, page.NextPageToken)
	require.Nil(t, page.Samples[0].HashrateHs)
	require.NotNil(t, page.Samples[0].PowerW)
	require.Zero(t, *page.Samples[0].PowerW)
	query = store.params[1]
	require.True(t, query.HasCursor)
	require.Equal(t, first.Time, query.AfterTime)
	require.Equal(t, first.CollectedAt, query.AfterCollectedAt)
	require.Equal(t, second.CollectedAt, page.Samples[0].CollectedAt.AsTime())
}

func TestModuleHistoryContractCursorCannotChangeQueryOrTier(t *testing.T) {
	for _, change := range []string{"organization", "authority", "module", "start", "end", "resolution"} {
		t.Run(change, func(t *testing.T) {
			req := moduleHistoryContractRequest()
			row := sqlc.ListContainerModuleHistoryRow{Time: req.StartTime.AsTime(), CollectedAt: req.StartTime.AsTime()}
			store := &moduleHistoryContractStore{allowed: true, rows: [][]sqlc.ListContainerModuleHistoryRow{{row, row}}}
			service := fleetinventory.NewService(store)
			org := int64(7)
			scopes := []fleetinventory.Scope{{OrgWide: true}}
			page, err := service.History(t.Context(), org, req, scopes)
			require.NoError(t, err)
			req.PageToken = page.NextPageToken
			switch change {
			case "organization":
				org++
			case "authority":
				scopes = []fleetinventory.Scope{{Sites: []int64{10}}}
			case "module":
				req.ModuleId++
			case "start":
				req.StartTime = timestamppb.New(row.Time.Add(time.Second))
			case "end":
				req.EndTime = timestamppb.New(row.Time.Add(time.Hour))
			case "resolution":
				store.resolution = 3600
			}
			response, err := service.History(t.Context(), org, req, scopes)
			require.Error(t, err)
			require.Nil(t, response)
			require.Len(t, store.params, 1, "invalid continuation must fail before data access")
		})
	}
}
