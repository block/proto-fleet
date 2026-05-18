package interfaces

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubBuildingStore is a minimal BuildingStore for the helper test.
// Only BuildingsByIDs is exercised; the embedded interface lets the
// other methods fall through to a nil dispatch.
type stubBuildingStore struct {
	BuildingStore
	owned map[int64]struct{}
}

func newOwnedStore(ids ...int64) *stubBuildingStore {
	s := &stubBuildingStore{owned: map[int64]struct{}{}}
	for _, id := range ids {
		s.owned[id] = struct{}{}
	}
	return s
}

func (s *stubBuildingStore) BuildingsByIDs(_ context.Context, _ int64, ids []int64) ([]int64, error) {
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := s.owned[id]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}

// recordingHandler is a slog.Handler that captures every record so
// tests can assert on the structured attrs (event, org_id,
// rejected_count). The cross-org audit log is the security signal
// monitoring relies on — drift here is silent unless we lock the
// shape down with a test.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *recordingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(_ string) slog.Handler      { return h }

// attrsByKey collapses a record's attrs into a key→value map. Slog
// records are streamed so we materialize them once per assertion.
func attrsByKey(r slog.Record) map[string]any {
	out := map[string]any{}
	r.Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value.Any()
		return true
	})
	return out
}

// installRecordingHandler swaps slog's default handler for the test
// and restores it on cleanup. Tests must not run in parallel because
// slog.Default is process-global.
func installRecordingHandler(t *testing.T) *recordingHandler {
	t.Helper()
	h := &recordingHandler{}
	old := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(old) })
	return h
}

func TestValidateFilterBuildings_CrossOrgEmitsAuditLog(t *testing.T) {
	// One owned (7) + one cross-org (99) — rejected_count is 1.
	handler := installRecordingHandler(t)
	store := newOwnedStore(7)

	err := ValidateFilterBuildings(context.Background(), 42, []int64{7, 99}, nil, store)

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	require.Len(t, handler.records, 1, "expected exactly one audit log record")
	rec := handler.records[0]
	assert.Equal(t, slog.LevelWarn, rec.Level)
	assert.Equal(t, "cross_org_filter_probe", rec.Message)
	attrs := attrsByKey(rec)
	assert.NotContains(t, attrs, "event", "event key is redundant with the log message")
	assert.Equal(t, int64(42), attrs["org_id"])
	assert.Equal(t, int64(1), attrs["rejected_count"])
}

func TestValidateFilterBuildings_CrossOrgZoneKey_EmitsAuditLog(t *testing.T) {
	// Same assertion shape but rejected via zone_keys instead of
	// building_ids — confirms the audit fires on either path.
	handler := installRecordingHandler(t)
	store := newOwnedStore(7)

	err := ValidateFilterBuildings(
		context.Background(),
		42,
		nil,
		[]ZoneKey{{BuildingID: 7, Zone: "Room 2"}, {BuildingID: 99, Zone: "Cold Aisle"}},
		store,
	)

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	require.Len(t, handler.records, 1)
	rec := handler.records[0]
	assert.Equal(t, "cross_org_filter_probe", rec.Message)
	attrs := attrsByKey(rec)
	assert.NotContains(t, attrs, "event", "event key is redundant with the log message")
	assert.Equal(t, int64(42), attrs["org_id"])
	assert.Equal(t, int64(1), attrs["rejected_count"])
}

func TestValidateFilterBuildings_HappyPath_NoAuditLog(t *testing.T) {
	// Confirm the audit log only fires on cross-org rejection — a
	// fully-owned request must be silent.
	handler := installRecordingHandler(t)
	store := newOwnedStore(7, 9)

	err := ValidateFilterBuildings(context.Background(), 42, []int64{7, 9}, nil, store)

	require.NoError(t, err)
	assert.Empty(t, handler.records, "happy path must not emit audit log")
}

func TestValidateFilterBuildings_AllWildcardZones_NoAuditLog(t *testing.T) {
	// Wildcard ZoneKeys (BuildingID == 0) skip the cross-org check;
	// they neither call the store nor emit a log.
	handler := installRecordingHandler(t)

	err := ValidateFilterBuildings(
		context.Background(),
		42,
		nil,
		[]ZoneKey{{BuildingID: 0, Zone: "any"}},
		nil,
	)

	require.NoError(t, err)
	assert.Empty(t, handler.records)
}

func TestValidateFilterBuildings_RejectedCountAggregates(t *testing.T) {
	// Three cross-org IDs (98, 99, 100), two owned (7, 9) → rejected_count = 3.
	// Confirms the count is the missing set size, not 1.
	handler := installRecordingHandler(t)
	store := newOwnedStore(7, 9)

	err := ValidateFilterBuildings(
		context.Background(),
		42,
		[]int64{7, 9, 98, 99, 100},
		nil,
		store,
	)

	require.Error(t, err)
	require.Len(t, handler.records, 1)
	attrs := attrsByKey(handler.records[0])
	assert.Equal(t, int64(3), attrs["rejected_count"])
}
