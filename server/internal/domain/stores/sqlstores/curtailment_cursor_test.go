package sqlstores

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// TestCurtailmentEventCursor_RoundTrip: encode then decode returns the
// same query-bound cursor state.
func TestCurtailmentEventCursor_RoundTrip(t *testing.T) {
	t.Parallel()
	encoded := encodeCurtailmentEventCursor(&curtailmentEventCursor{
		ID:          12345,
		OrgID:       42,
		StateFilter: models.EventStateActive,
	})
	require.NotEmpty(t, encoded)

	decoded, err := decodeCurtailmentEventCursor(encoded)
	require.NoError(t, err)
	require.NotNil(t, decoded)
	assert.Equal(t, int64(12345), decoded.ID)
	assert.Equal(t, int64(42), decoded.OrgID)
	assert.Equal(t, models.EventStateActive, decoded.StateFilter)
}

// TestCurtailmentEventCursor_RejectsNonPositiveID: a user-supplied token
// that decodes to zero or negative must reject with InvalidArgument so a
// malformed cursor doesn't silently rewind to the first page (id=0) or
// return zero rows (id<0). The store never emits a non-positive id.
func TestCurtailmentEventCursor_RejectsNonPositiveID(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
	}{
		{"zero id", `{"id":0,"org_id":42}`},
		{"negative id", `{"id":-1,"org_id":42}`},
		{"missing id (json default zero)", `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			token := base64.StdEncoding.EncodeToString([]byte(tc.body))
			_, err := decodeCurtailmentEventCursor(token)
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
			assert.Contains(t, err.Error(), "id must be > 0")
		})
	}
}

// TestCurtailmentEventCursor_RejectsNegativeOrgID: an explicit negative
// org_id is tampering — the store always emits non-negative values.
func TestCurtailmentEventCursor_RejectsNegativeOrgID(t *testing.T) {
	t.Parallel()
	token := base64.StdEncoding.EncodeToString([]byte(`{"id":123,"org_id":-1}`))
	_, err := decodeCurtailmentEventCursor(token)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "org_id must be >= 0")
}

// TestCurtailmentEventCursor_LegacyMissingOrgIDRestarts: pre-org-binding
// tokens omitted org_id and decode to the JSON zero value. The decoder
// returns a nil cursor so an in-flight pagination loop restarts from
// page 1 across the deployment boundary instead of failing.
func TestCurtailmentEventCursor_LegacyMissingOrgIDRestarts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
	}{
		{"missing org_id (json default zero)", `{"id":123}`},
		{"explicit zero org_id", `{"id":123,"org_id":0}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			token := base64.StdEncoding.EncodeToString([]byte(tc.body))
			decoded, err := decodeCurtailmentEventCursor(token)
			require.NoError(t, err)
			assert.Nil(t, decoded, "legacy token must decode to nil so ListEvents starts at page 1")
		})
	}
}

// TestCurtailmentEventCursor_RejectsBadEncoding: the proto-side max_len
// catches the size case; the codec still must reject malformed input.
func TestCurtailmentEventCursor_RejectsBadEncoding(t *testing.T) {
	t.Parallel()
	_, err := decodeCurtailmentEventCursor("not-valid-base64!!!")
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

// TestCurtailmentEventCursor_EmptyDecodesToNil: an empty string means
// "first page"; no error and no cursor.
func TestCurtailmentEventCursor_EmptyDecodesToNil(t *testing.T) {
	t.Parallel()
	decoded, err := decodeCurtailmentEventCursor("")
	require.NoError(t, err)
	assert.Nil(t, decoded)
}

// TestCurtailmentEventCursor_BindingFieldsRoundTrip: ListEvents compares
// (cursor.OrgID, cursor.StateFilter) against the current request's params
// and rejects mismatches as InvalidArgument. The guard relies on the codec
// preserving both fields verbatim — exercise the round-trip across the
// query-shapes ListEvents actually sees so a serialization regression on
// either side trips this test loudly.
func TestCurtailmentEventCursor_BindingFieldsRoundTrip(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		orgID       int64
		stateFilter models.EventState
	}{
		{"orgA-no-filter", 42, ""},
		{"orgA-active", 42, models.EventStateActive},
		{"orgA-pending", 42, models.EventStatePending},
		{"orgA-completed", 42, models.EventStateCompleted},
		{"orgB-active", 99, models.EventStateActive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			encoded := encodeCurtailmentEventCursor(&curtailmentEventCursor{
				ID:          1234,
				OrgID:       tc.orgID,
				StateFilter: tc.stateFilter,
			})
			require.NotEmpty(t, encoded)

			decoded, err := decodeCurtailmentEventCursor(encoded)
			require.NoError(t, err)
			require.NotNil(t, decoded)
			assert.Equal(t, tc.orgID, decoded.OrgID,
				"OrgID must round-trip — ListEvents rejects cross-org tokens by comparing this field")
			assert.Equal(t, tc.stateFilter, decoded.StateFilter,
				"StateFilter must round-trip — ListEvents rejects cross-filter tokens by comparing this field")
		})
	}
}
