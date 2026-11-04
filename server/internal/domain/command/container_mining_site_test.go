package command

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type containerMiningSiteMiner struct {
	interfaces.Miner
	orgID, siteID int64
	identifier    string
	calls         int
	request       sdk.ContainerMiningRequest
	onCall        func(context.Context)
}

func (m *containerMiningSiteMiner) GetOrgID() int64  { return m.orgID }
func (m *containerMiningSiteMiner) GetSiteID() int64 { return m.siteID }
func (m *containerMiningSiteMiner) GetID() models.DeviceIdentifier {
	return models.DeviceIdentifier(m.identifier)
}
func (m *containerMiningSiteMiner) ContainerMining(ctx context.Context, req sdk.ContainerMiningRequest) (sdk.ContainerMiningResult, error) {
	m.calls++
	m.request = req
	if m.onCall != nil {
		m.onCall(ctx)
	}
	return sdk.ContainerMiningResult{}, nil
}

type containerMiningSiteGetter struct {
	CachedMinerGetter
	miner interfaces.Miner
}

func (g containerMiningSiteGetter) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return g.miner, nil
}

type containerMiningSiteTest struct {
	conn                          *sql.DB
	q                             *sqlc.Queries
	orgID, deviceID, siteA, siteB int64
	identifier                    string
	session                       *session.Info
	svc                           *Service
	execution                     *ExecutionService
	messageQueue                  queue.MessageQueue
	miner                         *containerMiningSiteMiner
}

func setupContainerMiningSiteTest(t *testing.T) *containerMiningSiteTest {
	t.Helper()
	conn := newCommandTestDB(t)
	q := sqlc.New(conn)
	orgID, err := q.CreateOrganization(t.Context(), sqlc.CreateOrganizationParams{OrgID: "container-site-test", Name: "Container site test"})
	require.NoError(t, err)
	userID, err := q.CreateUser(t.Context(), sqlc.CreateUserParams{UserID: "container-site-user", Username: "container-site-user", PasswordHash: "unused", CreatedAt: time.Now()})
	require.NoError(t, err)
	const identifier = "container-site-controller"
	discoveredID, err := q.UpsertDiscoveredDevice(t.Context(), sqlc.UpsertDiscoveredDeviceParams{OrgID: orgID, DeviceIdentifier: identifier, DriverName: "proto", DeviceClass: 2, IpAddress: "192.0.2.10", Port: "8080", UrlScheme: "http", IsActive: true})
	require.NoError(t, err)
	deviceID, err := q.InsertDevice(t.Context(), sqlc.InsertDeviceParams{OrgID: orgID, DiscoveredDeviceID: discoveredID, DeviceIdentifier: identifier, MacAddress: "00:11:22:33:44:55"})
	require.NoError(t, err)
	_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: deviceID, PairingStatus: sqlc.PairingStatusEnumPAIRED})
	require.NoError(t, err)
	siteA, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: orgID, Name: "Site A", Slug: "site-a"})
	require.NoError(t, err)
	siteB, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: orgID, Name: "Site B", Slug: "site-b"})
	require.NoError(t, err)
	messageQueue := queue.NewDatabaseMessageQueue(&queue.Config{DequeLimit: 10, MaxFailureRetries: 3}, conn)
	miner := &containerMiningSiteMiner{orgID: orgID, identifier: identifier}
	config := &Config{MaxWorkers: 1}
	execution := NewExecutionService(config, conn, messageQueue, nil, nil, containerMiningSiteGetter{miner: miner}, nil, nil, nil)
	// Keep admission active without starting the background dequeue loop: this
	// test deliberately changes placement between durable enqueue and execution.
	run := newExecutionRun(t.Context())
	execution.run = run
	t.Cleanup(run.cancelAdmission)
	t.Cleanup(run.cancelWork)
	svc := NewService(config, conn, execution, messageQueue, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.startStatusUpdateRoutineOverride = func(string, onFinishedCallbackFunc) {}
	info := &session.Info{UserID: userID, OrganizationID: orgID, ExternalUserID: "container-site-user", Username: "container-site-user"}
	return &containerMiningSiteTest{conn: conn, q: q, orgID: orgID, deviceID: deviceID, siteA: siteA.ID, siteB: siteB.ID, identifier: identifier, session: info, svc: svc, execution: execution, messageQueue: messageQueue, miner: miner}
}

func (f *containerMiningSiteTest) move(t *testing.T, siteID int64) {
	t.Helper()
	rows, err := f.q.AssignContainersToSite(t.Context(), sqlc.AssignContainersToSiteParams{OrgID: f.orgID, Identifiers: []string{f.identifier}, TargetSiteID: sql.NullInt64{Int64: siteID, Valid: siteID != 0}})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
}

func (f *containerMiningSiteTest) enqueue(t *testing.T, req sdk.ContainerMiningRequest, authorizedSite int64) queue.Message {
	t.Helper()
	checks := 0
	result, err := f.svc.SetContainerMining(authn.SetInfo(t.Context(), f.session), f.identifier, req, func(_ context.Context, site *int64, actual sdk.ContainerMiningRequest) error {
		checks++
		if authorizedSite == 0 {
			require.Nil(t, site)
		} else {
			require.NotNil(t, site)
			require.Equal(t, authorizedSite, *site)
		}
		require.Equal(t, req, actual)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, checks)
	messages, err := f.messageQueue.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, result.BatchIdentifier, messages[0].BatchLogUUID)
	return messages[0]
}

func TestContainerMiningQueuedCommandRetainsAuthorizedSite(t *testing.T) {
	for _, operation := range []struct {
		name string
		req  sdk.ContainerMiningRequest
	}{
		{name: "sleep", req: sdk.ContainerMiningRequest{Action: "set_mining", Mining: new(false)}},
		{name: "wake", req: sdk.ContainerMiningRequest{Action: "set_mining", Mining: new(true)}},
		{name: "power", req: sdk.ContainerMiningRequest{Action: "set_power_target", Power: &sdk.ContainerPowerTarget{Watts: 100000}}},
	} {
		for _, placement := range []string{"different site", "unassigned to assigned", "assigned to unassigned", "unchanged site", "unchanged unassigned"} {
			t.Run(operation.name+"/"+placement, func(t *testing.T) {
				f := setupContainerMiningSiteTest(t)
				originalSite, executionSite := f.siteA, f.siteB
				switch placement {
				case "unassigned to assigned":
					originalSite = 0
				case "assigned to unassigned":
					executionSite = 0
				case "unchanged site":
					executionSite = originalSite
				case "unchanged unassigned":
					originalSite, executionSite = 0, 0
				}
				f.move(t, originalSite)
				// Retain a cached handle for the authorized site. Execution must
				// validate the current database placement rather than this cache.
				f.miner.siteID = originalSite
				message := f.enqueue(t, operation.req, originalSite)
				f.move(t, executionSite)
				_, _, executionErr := f.execution.executeQueuedCommand(t.Context(), message)
				if originalSite != executionSite {
					require.Zero(t, f.miner.calls, "a command authorized at a different site must never reach the controller")
					require.True(t, fleeterror.IsFailedPreconditionError(executionErr), "expected terminal FailedPrecondition, got %v", executionErr)
				} else {
					require.NoError(t, executionErr)
					require.Equal(t, 1, f.miner.calls)
					require.Equal(t, operation.req, f.miner.request)
				}
				updated, terminal, err := f.execution.markQueueMessageStatus(t.Context(), f.q, message, executionErr)
				require.NoError(t, err)
				require.True(t, updated)
				require.True(t, terminal, "site changes must fail permanently instead of retrying at the new site")
				stored, err := f.q.GetQueueMessagesByBatch(t.Context(), message.BatchLogUUID)
				require.NoError(t, err)
				require.Len(t, stored, 1)
				if originalSite != executionSite {
					require.Equal(t, sqlc.QueueStatusEnumFAILED, stored[0].Status)
				} else {
					require.Equal(t, sqlc.QueueStatusEnumSUCCESS, stored[0].Status)
				}
			})
		}
	}
}

// The placement lock must span the actual controller call. A check immediately
// before I/O still leaves a window for another request to move the controller.
func TestContainerMiningDispatchRetainsSiteLockThroughControllerCall(t *testing.T) {
	f := setupContainerMiningSiteTest(t)
	f.move(t, f.siteA)
	f.miner.siteID = f.siteA
	message := f.enqueue(t, sdk.ContainerMiningRequest{Action: "set_mining", Mining: new(false)}, f.siteA)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	done := make(chan struct{})
	t.Cleanup(func() {
		unblock()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("controller execution did not drain")
		}
	})
	f.miner.onCall = func(context.Context) { close(entered); <-release }
	finished := make(chan error, 1)
	go func() {
		defer close(done)
		_, _, err := f.execution.executeQueuedCommand(t.Context(), message)
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("controller call did not start")
	}
	// Use the competing assignment's deadline as the lock barrier, avoiding an
	// unbounded blocked goroutine if execution releases its placement lock early.
	moveCtx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	_, moveErr := f.q.AssignContainersToSite(moveCtx, sqlc.AssignContainersToSiteParams{OrgID: f.orgID, Identifiers: []string{f.identifier}, TargetSiteID: sql.NullInt64{Int64: f.siteB, Valid: true}})
	require.Error(t, moveErr, "site reassignment must wait until physical dispatch returns")
	require.ErrorIs(t, moveCtx.Err(), context.DeadlineExceeded)
	unblock()
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("controller execution did not finish")
	}
	require.Equal(t, 1, f.miner.calls)
	f.move(t, f.siteB)
}
