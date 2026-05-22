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

// TestCurtailmentEventCursor_LegacyMissingOrgIDRestartsFromFirstPage: a
// pre-OrgID-binding token (still in flight from before the cross-list-binding
// guard landed) must restart from the first page rather than reject, so a
// long-lived pagination loop that decoded a stale token recovers transparently.
func TestCurtailmentEventCursor_LegacyMissingOrgIDRestartsFromFirstPage(t *testing.T) {
	t.Parallel()
	token := base64.StdEncoding.EncodeToString([]byte(`{"id":123}`))
	decoded, err := decodeCurtailmentEventCursor(token)
	require.NoError(t, err)
	assert.Nil(t, decoded, "legacy token without org_id should restart from first page")
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
