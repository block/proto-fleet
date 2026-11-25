package curtailment

import (
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFacilityFanController_AuditFailuresAreCounted(t *testing.T) {
	recorder := newRecordingMetrics()
	audit := &recordingAuditLogger{logStrictErr: errors.New("audit unavailable")}
	controller, ok := NewFacilityFanController(nil, nil, nil, audit, recorder).(*facilityFanController)
	require.True(t, ok)
	event := &models.Event{OrgID: 1, EventUUID: uuid.New()}
	controller.logFailure(t.Context(), event, driver.PowerOff, "fan unavailable")
	controller.logSkipped(t.Context(), event, driver.PowerOff, []int64{1})
	assert.Equal(t, 1, recorder.auditWriteFailures[ActivityTypeFacilityFanCommandFailed])
	assert.Equal(t, 1, recorder.auditWriteFailures[ActivityTypeFacilityFanCommandSkipped])
}
