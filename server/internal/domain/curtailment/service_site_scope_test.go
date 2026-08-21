package curtailment

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// recordingAudit captures the activity rows the service emits so tests can
// assert site/org stamping.
type recordingAudit struct {
	events []activitymodels.Event
}

func (r *recordingAudit) Log(_ context.Context, e activitymodels.Event) {
	r.events = append(r.events, e)
}
func (r *recordingAudit) LogStrict(_ context.Context, e activitymodels.Event) error {
	r.events = append(r.events, e)
	return nil
}

func (r *recordingAudit) byType(t string) (activitymodels.Event, bool) {
	for _, e := range r.events {
		if e.Type == t {
			return e, true
		}
	}
	return activitymodels.Event{}, false
}

func TestStampCurtailmentSite(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		event      *models.Event
		wantSite   *int64
		wantOrgSet bool
	}{
		{
			name:       "site-scoped → stamps site_id and org",
			event:      &models.Event{OrgID: 3, ScopeType: models.ScopeTypeSite, ScopeJSON: []byte(`{"site_id":7}`)},
			wantSite:   ptrInt64(7),
			wantOrgSet: true,
		},
		{
			name:     "whole-org → no site (stays org-level)",
			event:    &models.Event{OrgID: 3, ScopeType: models.ScopeTypeWholeOrg, ScopeJSON: []byte(`{}`)},
			wantSite: nil,
		},
		{
			name:     "device-list → no single site",
			event:    &models.Event{OrgID: 3, ScopeType: models.ScopeTypeDeviceList, ScopeJSON: []byte(`{"device_identifiers":["a"]}`)},
			wantSite: nil,
		},
		{
			name:     "site scope but malformed JSON → no stamp (best effort)",
			event:    &models.Event{OrgID: 3, ScopeType: models.ScopeTypeSite, ScopeJSON: []byte(`not json`)},
			wantSite: nil,
		},
		{
			name:     "site scope but zero id → no stamp",
			event:    &models.Event{OrgID: 3, ScopeType: models.ScopeTypeSite, ScopeJSON: []byte(`{"site_id":0}`)},
			wantSite: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := activitymodels.Event{Category: activitymodels.CategoryCurtailment}
			stampCurtailmentSite(&row, tc.event)

			if tc.wantSite == nil {
				assert.Nil(t, row.SiteID)
			} else {
				require.NotNil(t, row.SiteID)
				assert.Equal(t, *tc.wantSite, *row.SiteID)
			}
			if tc.wantOrgSet {
				require.NotNil(t, row.OrganizationID)
				assert.Equal(t, tc.event.OrgID, *row.OrganizationID)
			}
		})
	}
}

// End-to-end: a site-scoped curtailment's admin-terminate lifecycle row must
// carry the site so it lands in /{site}/activity (regression: it previously
// emitted a NULL-site CategoryCurtailment row that, with curtailment now
// org-level, would surface only in the all-sites feed).
func TestService_AdminTerminate_StampsSiteForSiteScopedEvent(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	store := newFakeStore()
	store.adminTerminateResult = &models.Event{
		ID:        99,
		EventUUID: eventUUID,
		OrgID:     orgID,
		State:     models.EventStateCancelled,
		ScopeType: models.ScopeTypeSite,
		ScopeJSON: []byte(`{"site_id":42}`),
	}
	rec := &recordingAudit{}
	svc := NewService(store, WithAuditLogger(rec))

	_, err := svc.AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       orgID,
		EventUUID:   eventUUID,
		TargetState: models.EventStateCancelled,
		Reason:      "operator escalation",
	})
	require.NoError(t, err)

	row, ok := rec.byType(ActivityTypeAdminTerminated)
	require.True(t, ok, "expected an admin-terminated activity row")
	require.NotNil(t, row.SiteID, "site-scoped lifecycle row must stamp site_id")
	assert.Equal(t, int64(42), *row.SiteID)
	require.NotNil(t, row.OrganizationID, "site_id requires organization_id (FK/CHECK)")
	assert.Equal(t, orgID, *row.OrganizationID)
}

func TestService_AdminTerminate_NoSiteForWholeOrgEvent(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	store := newFakeStore()
	store.adminTerminateResult = &models.Event{
		ID:        99,
		EventUUID: eventUUID,
		OrgID:     orgID,
		State:     models.EventStateCancelled,
		ScopeType: models.ScopeTypeWholeOrg,
		ScopeJSON: []byte(`{}`),
	}
	rec := &recordingAudit{}
	svc := NewService(store, WithAuditLogger(rec))

	_, err := svc.AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       orgID,
		EventUUID:   eventUUID,
		TargetState: models.EventStateCancelled,
		Reason:      "fleet-wide stop",
	})
	require.NoError(t, err)

	row, ok := rec.byType(ActivityTypeAdminTerminated)
	require.True(t, ok)
	assert.Nil(t, row.SiteID, "whole-org curtailment has no single site")
}
