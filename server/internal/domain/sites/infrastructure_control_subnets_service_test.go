package sites

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"go.uber.org/mock/gomock"
)

func TestGetInfrastructureControlSubnetsReturnsPersistedCanonicalEntries(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	svc := NewService(store, nil, nil, nil, nil, nil, nil)

	store.EXPECT().
		GetInfrastructureControlSubnets(gomock.Any(), testOrgID, int64(11)).
		Return("10.1.2.0/24\nfd12:3456::8/128", nil)

	got, err := svc.GetInfrastructureControlSubnets(t.Context(), testOrgID, 11)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.1.2.0/24", "fd12:3456::8/128"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("unexpected subnets: got %v want %v", got, want)
	}
}

func expectInfrastructureControlSubnetMutationAllowed(store *mocks.MockSiteStore, siteID int64) {
	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, siteID).Return(nil)
	store.EXPECT().LockInfrastructureDevicesBySiteForWrite(inTxCtx, testOrgID, siteID).Return([]int64{70}, nil)
	store.EXPECT().CountNonTerminalCurtailmentEventsByInfrastructureDevices(inTxCtx, testOrgID, []int64{70}).Return(int64(0), nil)
}

func TestSetInfrastructureControlSubnetsPersistsCanonicalAndAuditsWithoutTopology(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	mockActivityStore := mocks.NewMockActivityStore(ctrl)
	var captured []activitymodels.Event
	mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
			captured = append(captured, *event)
			return nil
		})
	svc := NewService(store, nil, nil, nil, nil, &fakeTransactor{}, activity.NewService(mockActivityStore))

	expectInfrastructureControlSubnetMutationAllowed(store, 11)
	store.EXPECT().
		SetInfrastructureControlSubnets(
			gomock.Any(),
			testOrgID,
			int64(11),
			"10.42.8.0/24\nfd12:3456::8/128",
		).
		Return("10.42.8.0/24\nfd12:3456::8/128", nil)

	got, err := svc.SetInfrastructureControlSubnets(t.Context(), testOrgID, 11, []string{
		"fd12:3456::8/128",
		"10.42.8.99/24",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.42.8.0/24", "fd12:3456::8/128"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("unexpected canonical subnets: got %v want %v", got, want)
	}

	if len(captured) != 1 {
		t.Fatalf("expected one audit event, got %d", len(captured))
	}
	event := captured[0]
	if event.Type != eventControlSubnetsCommissioned {
		t.Fatalf("unexpected event type %q", event.Type)
	}
	if len(event.Metadata) != 2 ||
		event.Metadata["site_id"] != int64(11) ||
		event.Metadata["prefix_count"] != 2 {
		t.Fatalf("audit metadata must contain only site_id and prefix_count: %#v", event.Metadata)
	}
	serialized := fmt.Sprintf("%#v", event)
	for _, secret := range []string{"10.42.8", "fd12:3456"} {
		if contains(serialized, secret) {
			t.Fatalf("audit event leaked OT topology %q: %s", secret, serialized)
		}
	}
}

func TestSetInfrastructureControlSubnetsClearAuditsDecommission(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	mockActivityStore := mocks.NewMockActivityStore(ctrl)
	var captured activitymodels.Event
	mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
			captured = *event
			return nil
		})
	svc := NewService(store, nil, nil, nil, nil, &fakeTransactor{}, activity.NewService(mockActivityStore))

	expectInfrastructureControlSubnetMutationAllowed(store, 11)
	store.EXPECT().
		SetInfrastructureControlSubnets(gomock.Any(), testOrgID, int64(11), "").
		Return("", nil)

	got, err := svc.SetInfrastructureControlSubnets(t.Context(), testOrgID, 11, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("clear must return an empty allowlist, got %v", got)
	}
	if captured.Type != eventControlSubnetsDecommissioned ||
		captured.Metadata["prefix_count"] != 0 {
		t.Fatalf("unexpected decommission audit: %#v", captured)
	}
}

func TestSetInfrastructureControlSubnetsFailedWriteDoesNotAudit(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	mockActivityStore := mocks.NewMockActivityStore(ctrl)
	// No activity Insert expectation: any audit attempt after the failed write
	// fails this test.
	svc := NewService(store, nil, nil, nil, nil, &fakeTransactor{}, activity.NewService(mockActivityStore))

	writeErr := errors.New("write failed")
	expectInfrastructureControlSubnetMutationAllowed(store, 11)
	store.EXPECT().
		SetInfrastructureControlSubnets(gomock.Any(), testOrgID, int64(11), "10.42.8.0/24").
		Return("", writeErr)

	_, err := svc.SetInfrastructureControlSubnets(
		t.Context(),
		testOrgID,
		11,
		[]string{"10.42.8.0/24"},
	)
	if !errors.Is(err, writeErr) {
		t.Fatalf("expected write error, got %v", err)
	}
}

func TestSetInfrastructureControlSubnetsFailedAuditFailsMutation(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	mockActivityStore := mocks.NewMockActivityStore(ctrl)
	store.EXPECT().
		SetInfrastructureControlSubnets(gomock.Any(), testOrgID, int64(11), "10.42.8.0/24").
		Return("10.42.8.0/24", nil)
	mockActivityStore.EXPECT().
		Insert(gomock.Any(), gomock.Any()).
		Return(errors.New("audit unavailable"))

	svc := NewService(
		store,
		nil,
		nil,
		nil,
		nil,
		&fakeTransactor{},
		activity.NewService(mockActivityStore),
	)
	expectInfrastructureControlSubnetMutationAllowed(store, 11)
	_, err := svc.SetInfrastructureControlSubnets(
		t.Context(),
		testOrgID,
		11,
		[]string{"10.42.8.0/24"},
	)
	if err == nil || !strings.Contains(err.Error(), "failed to audit") {
		t.Fatalf("expected audit failure, got %v", err)
	}
}

func TestSetInfrastructureControlSubnetsRejectsClaimedFacilityFans(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	svc := NewService(store, nil, nil, nil, nil, &fakeTransactor{}, activity.NewService(mocks.NewMockActivityStore(ctrl)))

	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, int64(11)).Return(nil)
	store.EXPECT().LockInfrastructureDevicesBySiteForWrite(inTxCtx, testOrgID, int64(11)).Return([]int64{70}, nil)
	store.EXPECT().CountNonTerminalCurtailmentEventsByInfrastructureDevices(inTxCtx, testOrgID, []int64{70}).Return(int64(1), nil)

	_, err := svc.SetInfrastructureControlSubnets(t.Context(), testOrgID, 11, []string{"10.42.8.0/24"})
	if err == nil || !fleeterror.IsFailedPreconditionError(err) {
		t.Fatalf("expected claimed-fan precondition failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "active curtailment events") {
		t.Fatalf("unexpected error: %v", err)
	}
}
