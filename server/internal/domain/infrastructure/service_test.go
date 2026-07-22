package infrastructure_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

// auditHarness wires the service against mock stores plus a capturing
// activity sink so tests can pin the audit trail for device mutations.
type auditHarness struct {
	svc      *infrastructure.Service
	store    *mocks.MockInfrastructureDeviceStore
	captured *[]activitymodels.Event
}

func newAuditHarness(t *testing.T) *auditHarness {
	t.Helper()
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInfrastructureDeviceStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	siteStore.EXPECT().LockSiteForWrite(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	tx := mocks.NewMockTransactor(ctrl)
	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	)

	captured := []activitymodels.Event{}
	activityStore := mocks.NewMockActivityStore(ctrl)
	activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
			captured = append(captured, *event)
			return nil
		}).AnyTimes()

	svc := infrastructure.NewService(store, siteStore, infrastructure.NewDefaultDriverRegistry(), tx, activity.NewService(activityStore))
	return &auditHarness{svc: svc, store: store, captured: &captured}
}

func auditDevice() *models.Device {
	return &models.Device{
		ID:           7,
		OrgID:        testOrgID,
		SiteID:       testSiteID,
		BuildingName: "Building 1",
		RackName:     "Rack A1",
		Name:         "Zone A exhaust fans",
		DeviceKind:   models.KindFanGroup,
		FanCount:     12,
		Enabled:      true,
		DriverType:   "modbus_tcp",
		DriverConfig: validModbusConfig(),
	}
}

// requireAuditEvent asserts the single captured event matches the
// expected type and carries the protocol-blind metadata — and, per the
// security review, never the driver_config (OT control topology).
func requireAuditEvent(t *testing.T, captured []activitymodels.Event, eventType string) {
	t.Helper()
	require.Len(t, captured, 1)
	event := captured[0]
	assert.Equal(t, eventType, event.Type)
	assert.Equal(t, activitymodels.CategoryFleetManagement, event.Category)
	require.NotNil(t, event.OrganizationID)
	assert.Equal(t, testOrgID, *event.OrganizationID)
	require.NotNil(t, event.SiteID)
	assert.Equal(t, testSiteID, *event.SiteID)
	assert.Contains(t, event.Description, `"Zone A exhaust fans"`)
	assert.Equal(t, int64(7), event.Metadata["infrastructure_device_id"])
	assert.Equal(t, "modbus_tcp", event.Metadata["driver_type"])
	assert.Equal(t, "Rack A1", event.Metadata["rack_name"])
	assert.NotContains(t, event.Metadata, "driver_config",
		"audit metadata must not carry OT control topology")
	assert.NotContains(t, event.Description, "10.1.2.3",
		"audit description must not echo OT endpoints")
}

func TestService_CreateEmitsAuditEvent(t *testing.T) {
	t.Parallel()
	h := newAuditHarness(t)
	h.store.EXPECT().CreateInfrastructureDevice(gomock.Any(), gomock.Any()).Return(auditDevice(), nil)

	_, err := h.svc.Create(context.Background(), createParams(nil))
	require.NoError(t, err)
	requireAuditEvent(t, *h.captured, "infrastructure_device.created")
}

func TestService_UpdateEmitsAuditEvent(t *testing.T) {
	t.Parallel()
	h := newAuditHarness(t)
	h.store.EXPECT().LockInfrastructureDeviceForWrite(gomock.Any(), testOrgID, int64(7), testSiteID).Return(nil)
	h.store.EXPECT().GetInfrastructureDevice(gomock.Any(), testOrgID, int64(7)).Return(auditDevice(), nil)
	h.store.EXPECT().UpdateInfrastructureDevice(gomock.Any(), gomock.Any()).Return(auditDevice(), nil)

	_, err := h.svc.Update(context.Background(), models.UpdateParams{
		OrgID:          testOrgID,
		ID:             7,
		ExpectedSiteID: testSiteID,
		SiteID:         testSiteID,
		BuildingName:   "Building 1",
		Name:           "Zone A exhaust fans",
		DeviceKind:     models.KindFanGroup,
		FanCount:       12,
		Enabled:        boolPtr(true),
		DriverType:     "modbus_tcp",
		DriverConfig:   validModbusConfig(),
	})
	require.NoError(t, err)
	requireAuditEvent(t, *h.captured, "infrastructure_device.updated")
}

func TestService_UpdateRejectsDisablingDeviceClaimedByActiveCurtailmentEvent(t *testing.T) {
	t.Parallel()
	h := newAuditHarness(t)
	h.store.EXPECT().LockInfrastructureDeviceForWrite(gomock.Any(), testOrgID, int64(7), testSiteID).Return(nil)
	h.store.EXPECT().GetInfrastructureDevice(gomock.Any(), testOrgID, int64(7)).Return(auditDevice(), nil)
	h.store.EXPECT().CountActiveCurtailmentEventsByInfrastructureDevice(gomock.Any(), testOrgID, int64(7)).Return(int64(1), nil)

	_, err := h.svc.Update(context.Background(), models.UpdateParams{
		OrgID:          testOrgID,
		ID:             7,
		ExpectedSiteID: testSiteID,
		SiteID:         testSiteID,
		BuildingName:   "Building 1",
		Name:           "Zone A exhaust fans",
		DeviceKind:     models.KindFanGroup,
		FanCount:       12,
		Enabled:        boolPtr(false),
		DriverType:     "modbus_tcp",
		DriverConfig:   validModbusConfig(),
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "enabled state or driver configuration")
	assert.Empty(t, *h.captured)
}

func TestService_UpdateRejectsCommandChangeProtectedByTerminalFanRecovery(t *testing.T) {
	t.Parallel()
	h := newAuditHarness(t)
	current := auditDevice()
	current.Enabled = false
	h.store.EXPECT().LockInfrastructureDeviceForWrite(gomock.Any(), testOrgID, int64(7), testSiteID).Return(nil)
	h.store.EXPECT().GetInfrastructureDevice(gomock.Any(), testOrgID, int64(7)).Return(current, nil)
	h.store.EXPECT().CountActiveCurtailmentEventsByInfrastructureDevice(gomock.Any(), testOrgID, int64(7)).Return(int64(1), nil)

	_, err := h.svc.Update(context.Background(), models.UpdateParams{
		OrgID:          testOrgID,
		ID:             7,
		ExpectedSiteID: testSiteID,
		SiteID:         testSiteID,
		BuildingName:   "Building 1",
		Name:           "Zone A exhaust fans",
		DeviceKind:     models.KindFanGroup,
		FanCount:       12,
		Enabled:        boolPtr(true),
		DriverType:     "modbus_tcp",
		DriverConfig:   validModbusConfig(),
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "unresolved terminal facility fan recovery")
	assert.Empty(t, *h.captured)
}

func TestService_UpdateAllowsDisplayOnlyChangeForClaimedDevice(t *testing.T) {
	t.Parallel()
	h := newAuditHarness(t)
	current := auditDevice()
	updated := auditDevice()
	updated.Name = "Renamed exhaust fans"
	h.store.EXPECT().LockInfrastructureDeviceForWrite(gomock.Any(), testOrgID, int64(7), testSiteID).Return(nil)
	h.store.EXPECT().GetInfrastructureDevice(gomock.Any(), testOrgID, int64(7)).Return(current, nil)
	h.store.EXPECT().UpdateInfrastructureDevice(gomock.Any(), gomock.Any()).Return(updated, nil)

	got, err := h.svc.Update(context.Background(), models.UpdateParams{
		OrgID:          testOrgID,
		ID:             7,
		ExpectedSiteID: testSiteID,
		SiteID:         testSiteID,
		BuildingName:   "Building 1",
		Name:           "Renamed exhaust fans",
		DeviceKind:     models.KindFanGroup,
		FanCount:       12,
		Enabled:        boolPtr(true),
		DriverType:     "modbus_tcp",
		DriverConfig:   validModbusConfig(),
	})

	require.NoError(t, err)
	assert.Equal(t, "Renamed exhaust fans", got.Name)
	require.Len(t, *h.captured, 1)
	assert.Equal(t, "infrastructure_device.updated", (*h.captured)[0].Type)
	assert.Contains(t, (*h.captured)[0].Description, `"Renamed exhaust fans"`)
}

func TestService_DeleteEmitsAuditEvent(t *testing.T) {
	t.Parallel()
	h := newAuditHarness(t)
	h.store.EXPECT().LockInfrastructureDeviceForWrite(gomock.Any(), testOrgID, int64(7), testSiteID).Return(nil)
	h.store.EXPECT().CountResponseProfilesByInfrastructureDevice(gomock.Any(), testOrgID, int64(7)).Return(int64(0), nil)
	h.store.EXPECT().CountActiveCurtailmentEventsByInfrastructureDevice(gomock.Any(), testOrgID, int64(7)).Return(int64(0), nil)
	h.store.EXPECT().SoftDeleteInfrastructureDevice(gomock.Any(), testOrgID, int64(7), testSiteID).
		Return(auditDevice(), true, nil)

	require.NoError(t, h.svc.Delete(context.Background(), testOrgID, 7, testSiteID))
	requireAuditEvent(t, *h.captured, "infrastructure_device.deleted")
}

func TestService_DeleteNotFoundEmitsNoAuditEvent(t *testing.T) {
	t.Parallel()
	h := newAuditHarness(t)
	h.store.EXPECT().LockInfrastructureDeviceForWrite(gomock.Any(), testOrgID, int64(7), testSiteID).Return(nil)
	h.store.EXPECT().CountResponseProfilesByInfrastructureDevice(gomock.Any(), testOrgID, int64(7)).Return(int64(0), nil)
	h.store.EXPECT().CountActiveCurtailmentEventsByInfrastructureDevice(gomock.Any(), testOrgID, int64(7)).Return(int64(0), nil)
	h.store.EXPECT().SoftDeleteInfrastructureDevice(gomock.Any(), testOrgID, int64(7), testSiteID).
		Return(nil, false, nil)

	err := h.svc.Delete(context.Background(), testOrgID, 7, testSiteID)
	require.Error(t, err)
	assert.Empty(t, *h.captured, "failed mutations must not emit audit events")
}

func TestService_DeleteRejectsDeviceReferencedByResponseProfile(t *testing.T) {
	t.Parallel()
	h := newAuditHarness(t)
	h.store.EXPECT().LockInfrastructureDeviceForWrite(gomock.Any(), testOrgID, int64(7), testSiteID).Return(nil)
	h.store.EXPECT().CountResponseProfilesByInfrastructureDevice(gomock.Any(), testOrgID, int64(7)).Return(int64(1), nil)

	err := h.svc.Delete(context.Background(), testOrgID, 7, testSiteID)

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "update those profiles first")
	assert.Empty(t, *h.captured)
}

func TestService_UpdateRejectsMovingDeviceReferencedByResponseProfile(t *testing.T) {
	t.Parallel()
	h := newAuditHarness(t)
	h.store.EXPECT().LockInfrastructureDeviceForWrite(gomock.Any(), testOrgID, int64(7), testSiteID).Return(nil)
	h.store.EXPECT().CountResponseProfilesByInfrastructureDevice(gomock.Any(), testOrgID, int64(7)).Return(int64(1), nil)

	_, err := h.svc.Update(context.Background(), models.UpdateParams{
		OrgID:          testOrgID,
		ID:             7,
		ExpectedSiteID: testSiteID,
		SiteID:         testSiteID + 1,
		BuildingName:   "Building 1",
		Name:           "Zone A exhaust fans",
		DeviceKind:     models.KindFanGroup,
		FanCount:       12,
		Enabled:        boolPtr(true),
		DriverType:     "modbus_tcp",
		DriverConfig:   validModbusConfig(),
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "before moving it")
	assert.Empty(t, *h.captured)
}

func TestService_DeleteRejectsDeviceClaimedByActiveCurtailmentEvent(t *testing.T) {
	t.Parallel()
	h := newAuditHarness(t)
	h.store.EXPECT().LockInfrastructureDeviceForWrite(gomock.Any(), testOrgID, int64(7), testSiteID).Return(nil)
	h.store.EXPECT().CountResponseProfilesByInfrastructureDevice(gomock.Any(), testOrgID, int64(7)).Return(int64(0), nil)
	h.store.EXPECT().CountActiveCurtailmentEventsByInfrastructureDevice(gomock.Any(), testOrgID, int64(7)).Return(int64(1), nil)

	err := h.svc.Delete(context.Background(), testOrgID, 7, testSiteID)

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "active curtailment event")
	assert.Empty(t, *h.captured)
}

func TestService_UpdateRejectsMovingDeviceClaimedByActiveCurtailmentEvent(t *testing.T) {
	t.Parallel()
	h := newAuditHarness(t)
	h.store.EXPECT().LockInfrastructureDeviceForWrite(gomock.Any(), testOrgID, int64(7), testSiteID).Return(nil)
	h.store.EXPECT().CountResponseProfilesByInfrastructureDevice(gomock.Any(), testOrgID, int64(7)).Return(int64(0), nil)
	h.store.EXPECT().CountActiveCurtailmentEventsByInfrastructureDevice(gomock.Any(), testOrgID, int64(7)).Return(int64(1), nil)

	_, err := h.svc.Update(context.Background(), models.UpdateParams{
		OrgID:          testOrgID,
		ID:             7,
		ExpectedSiteID: testSiteID,
		SiteID:         testSiteID + 1,
		BuildingName:   "Building 1",
		Name:           "Zone A exhaust fans",
		DeviceKind:     models.KindFanGroup,
		FanCount:       12,
		Enabled:        boolPtr(true),
		DriverType:     "modbus_tcp",
		DriverConfig:   validModbusConfig(),
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "active curtailment event")
	assert.Empty(t, *h.captured)
}
