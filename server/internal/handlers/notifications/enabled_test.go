package notifications

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnabledHandler(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		rec := httptest.NewRecorder()
		NewEnabledHandler(enabled)(rec, httptest.NewRequest(http.MethodGet, "/api/v1/notifications/enabled", nil))

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		var got EnabledResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, enabled, got.Enabled)
	}
}
