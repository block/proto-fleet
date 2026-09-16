package rollout

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnforcementSessionHasBatchOwnerWithoutHumanIdentity(t *testing.T) {
	svc := &Service{}
	ctx := svc.enforcementContext(t.Context(), sqlc.FirmwareRollout{OrgID: 7}, 42)
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(42), info.UserID)
	assert.Equal(t, int64(7), info.OrganizationID)
	assert.Equal(t, session.ActorRolloutEnforcement, info.Actor)
	assert.Empty(t, info.ExternalUserID)
	assert.Empty(t, info.Username)
}
