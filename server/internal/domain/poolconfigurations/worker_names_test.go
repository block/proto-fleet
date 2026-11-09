package poolconfigurations

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Model the equipment boundary using live worker rendering inputs. This keeps
// database transactions, review immutability, and assignment fencing real.
type workerNameEquipment struct{ rejectWorker string }

func (e *workerNameEquipment) ReviewTargets(ctx context.Context, q sqlc.Querier, scope Scope, _ int64, targets []AssignmentTarget, _ []Endpoint) ([]TargetApproval, error) {
	approvals := make([]TargetApproval, len(targets))
	for i, target := range targets {
		if err := q.EnsureDeviceWriteFences(ctx, []int64{target.DeviceID}); err != nil {
			return nil, err
		}
		device, err := q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: scope.OrganizationID, ID: target.DeviceID})
		if err != nil {
			return nil, err
		}
		if e.rejectWorker != "" && device.WorkerName == e.rejectWorker {
			return nil, fleeterror.NewFailedPreconditionError("desired worker cannot be applied")
		}
		binding, err := json.Marshal(struct {
			Device sqlc.GetPoolConfigurationRuntimeRow
		}{Device: device})
		if err != nil {
			return nil, fmt.Errorf("encode worker fixture binding: %w", err)
		}
		approvals[i] = TargetApproval{Target: target, Binding: string(binding)}
	}
	return approvals, nil
}

func (*workerNameEquipment) VerifyPoolEquipmentCredentials(context.Context, string, string) (CredentialProof, error) {
	return func(context.Context, sqlc.Querier) error { return nil }, nil
}

type workerNameFixture struct {
	conn       *sql.DB
	scope      Scope
	repo       *Repository
	service    *Service
	equipment  *workerNameEquipment
	target     AssignmentTarget
	config     Configuration
	identifier string
	actorID    int64
	roleID     int64
}

func newWorkerNameFixture(t *testing.T) workerNameFixture {
	t.Helper()
	conn, scope, repo, target := assignmentFixture(t)
	ctx := t.Context()
	require.NoError(t, authz.Reconcile(ctx, conn))
	var actorID int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO "user"(user_id,username,password_hash) VALUES($1,$2,'unused') RETURNING id`, uuid.NewString(), uuid.NewString()).Scan(&actorID))
	q := sqlc.New(conn)
	role, err := q.GetBuiltinRoleForOrg(ctx, sqlc.GetBuiltinRoleForOrgParams{OrganizationID: sql.NullInt64{Int64: scope.OrganizationID, Valid: true}, BuiltinKey: sql.NullString{String: "SUPER_ADMIN", Valid: true}})
	require.NoError(t, err)
	_, err = q.AssignRole(ctx, sqlc.AssignRoleParams{UserID: actorID, OrganizationID: scope.OrganizationID, RoleID: role.ID, ScopeType: "org"})
	require.NoError(t, err)
	var identifier string
	require.NoError(t, conn.QueryRowContext(ctx, `UPDATE device SET worker_name='old-worker' WHERE id=$1 RETURNING device_identifier`, target.DeviceID).Scan(&identifier))
	equipment := &workerNameEquipment{}
	service := NewService(conn, repo, selectionAuthority{Principal{Scope: scope, ActorID: actorID}}, equipment)
	config, err := service.Create(ctx, "", RevisionOptions{Name: "Production", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout", WorkerTemplate: "{miner_name}"}}})
	require.NoError(t, err)
	_, err = db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Operation, error) {
		return repo.Assign(ctx, q, scope, actorID, config.ID, config.RevisionID, []AssignmentChange{{Target: target}})
	})
	require.NoError(t, err)
	return workerNameFixture{conn: conn, scope: scope, repo: repo, service: service, equipment: equipment, target: target, config: config, identifier: identifier, actorID: actorID, roleID: role.ID}
}

func (f workerNameFixture) worker(t *testing.T) string {
	t.Helper()
	var name string
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT worker_name FROM device WHERE id=$1`, f.target.ID).Scan(&name))
	return name
}

func TestWorkerNameReviewDefersMutationAndConfirmsCanonicalIntentAtomically(t *testing.T) {
	f := newWorkerNameFixture(t)
	ctx := t.Context()
	before := poolMaintenanceState(t, f.conn, f.scope.OrganizationID)
	reviews, err := f.service.ReviewWorkerNames(ctx, map[string]string{f.identifier: "new-worker"})
	require.NoError(t, err)
	require.Len(t, reviews, 1)
	review := reviews[0]
	require.Equal(t, f.config.ID, review.Configuration.ID)
	require.Equal(t, []WorkerNameChange{{DeviceIdentifier: f.identifier, Before: "old-worker", After: "new-worker"}}, review.WorkerNames)
	require.Equal(t, "old-worker", f.worker(t))
	after := poolMaintenanceState(t, f.conn, f.scope.OrganizationID)
	for table, rows := range before {
		if table != "pool_configuration_review" {
			require.Equal(t, rows, after[table], "review must not mutate %s", table)
		}
	}
	result, err := f.service.ConfirmReview(ctx, "", review.ID, Confirmation{Password: "actor-password"})
	require.NoError(t, err)
	require.NotNil(t, result.Operation)
	require.Equal(t, f.config.RevisionID, result.Configuration.RevisionID, "worker changes retain the reviewed configuration revision")
	require.Equal(t, "new-worker", f.worker(t))
	require.Len(t, result.Operation.Targets, 1)
	var binding string
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT reviewed_binding FROM pool_configuration_operation_target WHERE operation_id=$1`, result.Operation.ID).Scan(&binding))
	var retained struct {
		Device sqlc.GetPoolConfigurationRuntimeRow
	}
	require.NoError(t, json.Unmarshal([]byte(binding), &retained))
	require.Equal(t, "new-worker", retained.Device.WorkerName, "queued work must render the confirmed worker name")
	state := poolMaintenanceState(t, f.conn, f.scope.OrganizationID)
	replay, err := f.service.ConfirmReview(ctx, "", review.ID, Confirmation{Password: "actor-password"})
	require.NoError(t, err)
	require.True(t, replay.Replay)
	require.Equal(t, result.Operation.ID, replay.Operation.ID)
	require.Equal(t, state, poolMaintenanceState(t, f.conn, f.scope.OrganizationID), "confirmation replay cannot enqueue work or save metadata again")
}

func TestWorkerNameConfirmationRejectsChangedWorkerOrAssignment(t *testing.T) {
	for _, mutation := range []string{"worker", "assignment"} {
		t.Run(mutation, func(t *testing.T) {
			f := newWorkerNameFixture(t)
			ctx := t.Context()
			reviews, err := f.service.ReviewWorkerNames(ctx, map[string]string{f.identifier: "new-worker"})
			require.NoError(t, err)
			if mutation == "worker" {
				_, err = f.conn.ExecContext(ctx, `UPDATE device SET worker_name='intervening-worker' WHERE id=$1`, f.target.ID)
			} else {
				_, err = db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (Operation, error) {
					assignment, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.scope.OrganizationID, TargetKind: TargetDevice, TargetID: f.target.ID})
					if err != nil {
						return Operation{}, err
					}
					return f.repo.Assign(ctx, q, f.scope, f.actorID, f.config.ID, f.config.RevisionID, []AssignmentChange{{Target: f.target, ExpectedVersion: assignment.Version}})
				})
			}
			require.NoError(t, err)
			before := poolMaintenanceState(t, f.conn, f.scope.OrganizationID)
			oldWorker := f.worker(t)
			_, err = f.service.ConfirmReview(ctx, "", reviews[0].ID, Confirmation{Password: "actor-password"})
			require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
			require.Equal(t, oldWorker, f.worker(t))
			require.Equal(t, before, poolMaintenanceState(t, f.conn, f.scope.OrganizationID), "stale review rejection must not save partial work")
		})
	}
}

func TestWorkerNameConfirmationRollsBackNamesWhenNewBindingIsRejected(t *testing.T) {
	f := newWorkerNameFixture(t)
	ctx := t.Context()
	reviews, err := f.service.ReviewWorkerNames(ctx, map[string]string{f.identifier: "new-worker"})
	require.NoError(t, err)
	before := poolMaintenanceState(t, f.conn, f.scope.OrganizationID)
	f.equipment.rejectWorker = "new-worker"
	_, err = f.service.ConfirmReview(ctx, "", reviews[0].ID, Confirmation{Password: "actor-password"})
	require.ErrorContains(t, err, "desired worker cannot be applied")
	require.Equal(t, "old-worker", f.worker(t), "metadata update must roll back when canonical intent cannot be queued")
	require.Equal(t, before, poolMaintenanceState(t, f.conn, f.scope.OrganizationID))
}

func TestWorkerNameReviewAndConfirmationRequireLiveRenamePermission(t *testing.T) {
	f := newWorkerNameFixture(t)
	ctx := t.Context()
	reviews, err := f.service.ReviewWorkerNames(ctx, map[string]string{f.identifier: "new-worker"})
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `UPDATE user_organization_role SET deleted_at=now() WHERE user_id=$1 AND organization_id=$2`, f.actorID, f.scope.OrganizationID)
	require.NoError(t, err)
	before := poolMaintenanceState(t, f.conn, f.scope.OrganizationID)
	_, err = f.service.ReviewWorkerNames(ctx, map[string]string{f.identifier: "another-worker"})
	require.True(t, fleeterror.IsForbiddenError(err), "%v", err)
	_, err = f.service.ConfirmReview(ctx, "", reviews[0].ID, Confirmation{Password: "actor-password"})
	require.True(t, fleeterror.IsForbiddenError(err), "%v", err)
	require.Equal(t, "old-worker", f.worker(t))
	require.Equal(t, before, poolMaintenanceState(t, f.conn, f.scope.OrganizationID))
}

func TestWorkerNameReviewsGroupCurrentConfigurationsAndRejectUnassignedSelection(t *testing.T) {
	f := newWorkerNameFixture(t)
	ctx := t.Context()
	identifier := uuid.NewString()
	var discovery, deviceID int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name) VALUES($1,$2,'192.0.2.2','80','http','proto') RETURNING id`, f.scope.OrganizationID, identifier).Scan(&discovery))
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id,worker_name) VALUES($1,$2,'02:00:00:00:00:02',$3,'other-worker') RETURNING id`, f.scope.OrganizationID, identifier, discovery).Scan(&deviceID))
	names := map[string]string{f.identifier: "new-worker", identifier: "new-other-worker"}
	before := poolMaintenanceState(t, f.conn, f.scope.OrganizationID)
	_, err := f.service.ReviewWorkerNames(ctx, names)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
	require.Equal(t, before, poolMaintenanceState(t, f.conn, f.scope.OrganizationID), "mixed assigned and unassigned selection must not leave partial reviews")
	other, err := f.service.Create(ctx, "", RevisionOptions{Name: "Backup", Slots: []SlotOptions{{URL: "stratum+tcp://backup.example", Username: "payout", WorkerTemplate: "{miner_name}"}}})
	require.NoError(t, err)
	_, err = db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (Operation, error) {
		return f.repo.Assign(ctx, q, f.scope, f.actorID, other.ID, other.RevisionID, []AssignmentChange{{Target: AssignmentTarget{Kind: TargetDevice, ID: deviceID, DeviceID: deviceID}}})
	})
	require.NoError(t, err)
	reviews, err := f.service.ReviewWorkerNames(ctx, names)
	require.NoError(t, err)
	require.Len(t, reviews, 2)
	byConfig := map[string][]WorkerNameChange{}
	for _, review := range reviews {
		byConfig[review.Configuration.ID] = review.WorkerNames
	}
	require.Equal(t, []WorkerNameChange{{DeviceIdentifier: f.identifier, Before: "old-worker", After: "new-worker"}}, byConfig[f.config.ID])
	require.Equal(t, []WorkerNameChange{{DeviceIdentifier: identifier, Before: "other-worker", After: "new-other-worker"}}, byConfig[other.ID])
	require.Equal(t, "old-worker", f.worker(t))
}

func (f workerNameFixture) addDevice(t *testing.T, orgID int64) (string, AssignmentTarget) {
	t.Helper()
	identifier := uuid.NewString()
	var discovery, deviceID int64
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name) VALUES($1,$2,'192.0.2.2','80','http','proto') RETURNING id`, orgID, identifier).Scan(&discovery))
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id,worker_name) VALUES($1,$2,$4,$3,'old-worker') RETURNING id`, orgID, identifier, discovery, fmt.Sprintf("02:00:00:00:%02x:%02x", byte(discovery>>8), byte(discovery))).Scan(&deviceID))
	return identifier, AssignmentTarget{Kind: TargetDevice, ID: deviceID, DeviceID: deviceID}
}

func TestWorkerNameReviewBatchesAssignmentAndIdentifierLookups(t *testing.T) {
	for _, count := range []int{1, 32} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := newWorkerNameFixture(t)
			ctx := t.Context()
			names := map[string]string{f.identifier: "new-0"}
			var changes []AssignmentChange
			for i := 1; i < count; i++ {
				identifier, target := f.addDevice(t, f.scope.OrganizationID)
				names[identifier] = fmt.Sprintf("new-%d", i)
				changes = append(changes, AssignmentChange{Target: target})
			}
			if len(changes) > 0 {
				_, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (Operation, error) {
					return f.repo.Assign(ctx, q, f.scope, f.actorID, f.config.ID, f.config.RevisionID, changes)
				})
				require.NoError(t, err)
			}
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(provider)
			t.Cleanup(func() { otel.SetTracerProvider(previous); require.NoError(t, provider.Shutdown(context.Background())) })
			ctx, span := provider.Tracer("worker-name-review-test").Start(ctx, "review")
			defer span.End()
			reviews, err := f.service.ReviewWorkerNames(ctx, names)
			require.NoError(t, err)
			require.Len(t, reviews, 1)
			require.Len(t, reviews[0].WorkerNames, count)
			for _, change := range reviews[0].WorkerNames {
				require.Equal(t, "old-worker", change.Before)
				require.Equal(t, names[change.DeviceIdentifier], change.After)
			}
			counts := map[string]int{}
			for _, recorded := range recorder.Ended() {
				if recorded.SpanContext().TraceID() == span.SpanContext().TraceID() {
					counts[recorded.Name()]++
				}
			}
			require.Equal(t, 1, counts["db.GetOrgDeviceIDsWithIdentifiers"])
			require.Equal(t, 1, counts["db.ListPoolConfigurationAssignmentsForDevices"], "assignment discovery must use one batch regardless of selection size")
			require.Zero(t, counts["db.GetDeviceIdentifierByID"], "reuse identifiers from the organization-scoped lookup")
			require.Equal(t, count, counts["db.GetPoolConfigurationAssignment"], "only the existing later per-target equipment review reads remain")
		})
	}
}

func TestWorkerNameBatchReviewRejectsRetiredOrForeignSelections(t *testing.T) {
	for _, invalid := range []string{"retired", "other owner", "other organization"} {
		t.Run(invalid, func(t *testing.T) {
			f := newWorkerNameFixture(t)
			ctx := t.Context()
			scope := f.scope
			if invalid == "other organization" {
				require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Other') RETURNING id`, uuid.NewString()).Scan(&scope.OrganizationID))
			}
			identifier, target := f.addDevice(t, scope.OrganizationID)
			config := f.config
			if invalid == "other owner" {
				require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT $1,organization_id,id,1,'Customer',15 FROM hosted_account WHERE organization_id=$2 AND depth=0 RETURNING id`, uuid.NewString(), scope.OrganizationID).Scan(&scope.AccountID))
			}
			_, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (Operation, error) {
				if scope != f.scope {
					var err error
					config, err = f.repo.Create(ctx, q, scope, RevisionOptions{Name: "Other", Slots: []SlotOptions{{URL: "stratum+tcp://other.example", Username: "payout", WorkerTemplate: "{miner_name}"}}})
					if err != nil {
						return Operation{}, err
					}
				}
				return f.repo.Assign(ctx, q, scope, f.actorID, config.ID, config.RevisionID, []AssignmentChange{{Target: target}})
			})
			require.NoError(t, err)
			if invalid == "retired" {
				require.NoError(t, db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error {
					return f.repo.RetireAssignment(ctx, q, scope, target, 1)
				}))
			}
			before := poolMaintenanceState(t, f.conn, f.scope.OrganizationID)
			reviews, err := f.service.ReviewWorkerNames(ctx, map[string]string{f.identifier: "new-worker", identifier: "other-new-worker"})
			if invalid == "retired" {
				require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
			} else {
				require.True(t, fleeterror.IsForbiddenError(err), "%v", err)
			}
			require.Empty(t, reviews)
			require.Equal(t, before, poolMaintenanceState(t, f.conn, f.scope.OrganizationID), "reject the whole selection without saving partial reviews")
		})
	}
}

func (f workerNameFixture) setOutcome(t *testing.T, state, code string) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `UPDATE pool_configuration_operation_target SET state=$1,result_code=$2,updated_at=clock_timestamp() WHERE organization_id=$3 AND operation_id=(SELECT operation_id FROM pool_configuration_assignment WHERE organization_id=$3 AND target_kind='device' AND target_id=$4)`, state, code, f.scope.OrganizationID, f.target.ID)
	require.NoError(t, err)
}

func (f workerNameFixture) observe(t *testing.T, state string, fresh bool) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `INSERT INTO pool_configuration_observation(organization_id,target_kind,target_id,assignment_version,state,observed_at)
 SELECT organization_id,target_kind,target_id,version,$1,clock_timestamp() + CASE WHEN $2 THEN interval '1 second' ELSE interval '-1 hour' END
 FROM pool_configuration_assignment WHERE organization_id=$3`, state, fresh, f.scope.OrganizationID)
	require.NoError(t, err)
}

func TestWorkerNameMatchingReviewUsesCanonicalOutcome(t *testing.T) {
	for _, tt := range []struct {
		name, state, code, observation string
		fresh, retry, blocked          bool
	}{
		{name: "safe failure", state: "failed", retry: true},
		{name: "different", state: "different", retry: true},
		{name: "unverified", state: "applied_unverified", retry: true},
		{name: "verified", state: "verified"},
		{name: "pending", state: "pending"},
		{name: "applying", state: "applying"},
		{name: "uncertain", state: "failed", code: "write_uncertain", blocked: true},
		{name: "new verified observation", state: "failed", observation: "verified", fresh: true},
		{name: "new mismatch observation", state: "verified", observation: "different", fresh: true, retry: true},
		{name: "stale verified observation", state: "failed", observation: "verified", retry: true},
		{name: "pending with observation", state: "pending", observation: "different", fresh: true},
		{name: "uncertain with observation", state: "failed", code: "write_uncertain", observation: "different", fresh: true, blocked: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newWorkerNameFixture(t)
			f.setOutcome(t, tt.state, tt.code)
			if tt.observation != "" {
				f.observe(t, tt.observation, tt.fresh)
			}
			before := poolMaintenanceState(t, f.conn, f.scope.OrganizationID)
			reviews, err := f.service.ReviewWorkerNames(t.Context(), map[string]string{f.identifier: "old-worker"})
			if tt.blocked {
				require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
				require.Empty(t, reviews)
			} else {
				require.NoError(t, err)
				if tt.retry {
					require.Len(t, reviews, 1)
					require.Equal(t, []WorkerNameChange{{DeviceIdentifier: f.identifier, Before: "old-worker", After: "old-worker"}}, reviews[0].WorkerNames)
				} else {
					require.Empty(t, reviews)
				}
			}
			require.Equal(t, "old-worker", f.worker(t))
			if !tt.retry {
				require.Equal(t, before, poolMaintenanceState(t, f.conn, f.scope.OrganizationID))
			}
		})
	}
}

func TestWorkerNameMatchingRetryCreatesFreshIntentAndReplays(t *testing.T) {
	f := newWorkerNameFixture(t)
	ctx := t.Context()
	reviews, err := f.service.ReviewWorkerNames(ctx, map[string]string{f.identifier: "new-worker"})
	require.NoError(t, err)
	first, err := f.service.ConfirmReview(ctx, "", reviews[0].ID, Confirmation{Password: "actor-password"})
	require.NoError(t, err)
	f.setOutcome(t, "failed", "interrupted")
	reviews, err = f.service.ReviewWorkerNames(ctx, map[string]string{f.identifier: "new-worker"})
	require.NoError(t, err)
	require.Len(t, reviews, 1)
	retry, err := f.service.ConfirmReview(ctx, "", reviews[0].ID, Confirmation{Password: "actor-password"})
	require.NoError(t, err)
	require.NotEqual(t, first.Operation.ID, retry.Operation.ID)
	require.Equal(t, first.Configuration.RevisionID, retry.Configuration.RevisionID)
	require.Equal(t, "new-worker", f.worker(t))
	require.Equal(t, "pending", retry.Operation.Targets[0].State)
	replay, err := f.service.ConfirmReview(ctx, "", reviews[0].ID, Confirmation{Password: "actor-password"})
	require.NoError(t, err)
	require.True(t, replay.Replay)
	require.Equal(t, retry.Operation.ID, replay.Operation.ID)
}

func TestWorkerNameMatchingRetryRechecksOutcomeAtConfirmation(t *testing.T) {
	for _, outcome := range []string{"verified", "pending", "applying", "write_uncertain", "observation"} {
		t.Run(outcome, func(t *testing.T) {
			f := newWorkerNameFixture(t)
			f.setOutcome(t, "failed", "interrupted")
			reviews, err := f.service.ReviewWorkerNames(t.Context(), map[string]string{f.identifier: "old-worker"})
			require.NoError(t, err)
			require.Len(t, reviews, 1)
			switch outcome {
			case "write_uncertain":
				f.setOutcome(t, "failed", outcome)
			case "observation":
				f.observe(t, "verified", true)
			default:
				f.setOutcome(t, outcome, "")
			}
			before := poolMaintenanceState(t, f.conn, f.scope.OrganizationID)
			_, err = f.service.ConfirmReview(t.Context(), "", reviews[0].ID, Confirmation{Password: "actor-password"})
			require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
			require.Equal(t, before, poolMaintenanceState(t, f.conn, f.scope.OrganizationID))
		})
	}
}

func TestWorkerNameMatchingUnassignedDeviceIsUnchanged(t *testing.T) {
	f := newWorkerNameFixture(t)
	identifier, _ := f.addDevice(t, f.scope.OrganizationID)
	reviews, err := f.service.ReviewWorkerNames(t.Context(), map[string]string{identifier: "old-worker", f.identifier: "new-worker"})
	require.NoError(t, err)
	require.Len(t, reviews, 1)
	require.Len(t, reviews[0].WorkerNames, 1)
	require.Equal(t, f.identifier, reviews[0].WorkerNames[0].DeviceIdentifier)
}

func TestWorkerNameMatchingRetryRejectsNewConfigurationRevision(t *testing.T) {
	f := newWorkerNameFixture(t)
	ctx := t.Context()
	f.setOutcome(t, "failed", "interrupted")
	_, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (SaveResult, error) {
		return f.repo.Revise(ctx, q, f.scope, f.config.ID, f.config.Version, RevisionOptions{Name: "Production", Slots: []SlotOptions{{ID: f.config.Slots[0].ID, URL: "stratum+tcp://other.example", Username: "payout", WorkerTemplate: "{miner_name}", Password: new(string)}}})
	})
	require.NoError(t, err)
	before := poolMaintenanceState(t, f.conn, f.scope.OrganizationID)
	_, err = f.service.ReviewWorkerNames(ctx, map[string]string{f.identifier: "old-worker"})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
	require.Equal(t, before, poolMaintenanceState(t, f.conn, f.scope.OrganizationID))
}

func TestWorkerNameMatchingRetryIgnoresObservationFromOldAssignment(t *testing.T) {
	f := newWorkerNameFixture(t)
	f.setOutcome(t, "failed", "interrupted")
	f.observe(t, "verified", true)
	_, err := db.WithTransaction(t.Context(), f.conn, func(q sqlc.Querier) (Operation, error) {
		return f.repo.Assign(t.Context(), q, f.scope, f.actorID, f.config.ID, f.config.RevisionID, []AssignmentChange{{Target: f.target, ExpectedVersion: 1}})
	})
	require.NoError(t, err)
	f.setOutcome(t, "failed", "interrupted")
	reviews, err := f.service.ReviewWorkerNames(t.Context(), map[string]string{f.identifier: "old-worker"})
	require.NoError(t, err)
	require.Len(t, reviews, 1)
}
