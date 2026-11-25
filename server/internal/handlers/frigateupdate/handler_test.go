package frigateupdate

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/frigateupdate/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domain "github.com/block/proto-fleet/server/internal/domain/frigateupdate"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func TestHandler_RejectsMalformedIDsBeforeTheService(t *testing.T) {
	h := NewHandler(nil)
	ctx := context.Background()

	_, err := h.StartUpdate(ctx, connect.NewRequest(&pb.StartUpdateRequest{DeviceIdentifier: "d", PackageId: "nope", IdempotencyKey: "k"}))
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
	_, err = h.GetUpdate(ctx, connect.NewRequest(&pb.GetUpdateRequest{JobId: ""}))
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
	_, err = h.RetryUpdate(ctx, connect.NewRequest(&pb.RetryUpdateRequest{JobId: "x", IdempotencyKey: "k"}))
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
	_, err = h.CancelUpdate(ctx, connect.NewRequest(&pb.CancelUpdateRequest{JobId: "x", IdempotencyKey: "k"}))
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
	_, err = h.DeletePackage(ctx, connect.NewRequest(&pb.DeletePackageRequest{PackageId: "x"}))
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
	_, err = h.GetSupport(ctx, connect.NewRequest(&pb.GetSupportRequest{DeviceIdentifier: "d", PackageId: "x"}))
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
}

func TestJobToProto(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	id, pkgID := uuid.New(), uuid.New()
	job := domain.Job{
		DeviceIdentifier: "ct1-1",
		Package:          domain.Package{ID: pkgID, Version: "2.0.0", Architecture: "arm64", SizeBytes: 10, IssuedAt: now, UploadedAt: now},
		Row: sqlc.FrigateUpdateJob{
			ID: id, State: domain.StateFailed, RolloutPhase: string(sdk.FrigatePhaseFailed), FromVersion: "1.0.0", TargetVersion: "2.0.0",
			DriverInstanceID: "a", ErrorCode: "UPDATE_X", ErrorMessage: "boom", NextStep: "retry", Retryable: true, Cancellable: true,
			DisruptionStarted: true, ServedByMaintenance: true, ActorUsername: "alice", CreatedAt: now, UpdatedAt: now,
			LastContactAt: sql.NullTime{Time: now, Valid: true}, UnreachableSince: sql.NullTime{Time: now, Valid: true},
		},
		Instances: []sqlc.FrigateUpdateInstance{{
			InstanceID: "a", StageState: domain.StageStaged, StagedBytes: 10, SizeBytes: 10, AgentPhase: string(sdk.FrigateAgentFailed),
			LastContact: sql.NullTime{Time: now, Valid: true}, ContactStale: true, RunningVersion: "1.0.0", ErrorCode: "E",
		}},
	}
	out := jobToProto(job)
	assert.Equal(t, id.String(), out.JobId)
	assert.Equal(t, "ct1-1", out.DeviceIdentifier)
	assert.Equal(t, pb.FrigateUpdateState_FRIGATE_UPDATE_STATE_FAILED, out.State)
	assert.Equal(t, pb.FrigateRolloutPhase_FRIGATE_ROLLOUT_PHASE_FAILED, out.RolloutPhase)
	assert.True(t, out.Retryable && out.Cancellable && out.DisruptionStarted && out.ServedByMaintenance)
	assert.Equal(t, "alice", out.Actor)
	assert.NotNil(t, out.LastContact)
	assert.NotNil(t, out.UnreachableSince)
	assert.Nil(t, out.CompletedAt)
	assert.Equal(t, pkgID.String(), out.Package.Id)
	require.Len(t, out.Instances, 1)
	assert.Equal(t, pb.InstanceStageState_INSTANCE_STAGE_STATE_STAGED, out.Instances[0].StageState)
	assert.Equal(t, pb.FrigateAgentPhase_FRIGATE_AGENT_PHASE_FAILED, out.Instances[0].AgentPhase)
	assert.True(t, out.Instances[0].ContactStale)
}

func TestJobToProtoReportsAPendingCancelOnlyWhileStarting(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	requested := sql.NullTime{Time: now, Valid: true}
	starting := domain.Job{Row: sqlc.FrigateUpdateJob{ID: uuid.New(), State: domain.StateStarting, CancelRequestedAt: requested, CreatedAt: now, UpdatedAt: now}}
	out := jobToProto(starting)
	require.NotNil(t, out.CancelRequestedAt)
	assert.Equal(t, now, out.CancelRequestedAt.AsTime())

	// Once the job has ended the leftover request time is not a pending cancel.
	ended := starting
	ended.Row.State = domain.StateCancelled
	assert.Nil(t, jobToProto(ended).CancelRequestedAt)

	starting.Row.CancelRequestedAt = sql.NullTime{}
	assert.Nil(t, jobToProto(starting).CancelRequestedAt)
}

func TestEnumMappingsFallBackSafely(t *testing.T) {
	assert.Equal(t, pb.FrigateUpdateState_FRIGATE_UPDATE_STATE_UNSPECIFIED, stateToProto("weird"))
	assert.Equal(t, pb.FrigateRolloutPhase_FRIGATE_ROLLOUT_PHASE_UNKNOWN, phaseToProto("weird"))
	assert.Equal(t, pb.FrigateAgentPhase_FRIGATE_AGENT_PHASE_UNKNOWN, agentPhaseToProto("weird"))
	assert.Equal(t, pb.InstanceStageState_INSTANCE_STAGE_STATE_UNSPECIFIED, stageStateToProto("weird"))
	assert.Equal(t, pb.FrigateInstallationType_FRIGATE_INSTALLATION_TYPE_UNKNOWN, installationTypeToProto("weird"))
	assert.Equal(t, pb.FrigateInstallationType_FRIGATE_INSTALLATION_TYPE_DEBIAN_PACKAGE, installationTypeToProto(sdk.FrigateInstallationDebianPackage))
}

func TestInstanceSupportToProto(t *testing.T) {
	out := instanceSupportToProto(sdk.FrigateInstanceSupport{
		InstanceID: "a", PeerInstanceID: "b", RunningVersion: "1", Architecture: "arm64", InstallationType: sdk.FrigateInstallationDebianPackage,
		HelperAvailable: true, SelfCoordinationSupported: true, RemoteTriggerAllowed: true, SpoolFreeBytes: 5, Err: "x",
		TrustedReleaseKeyIDs: []string{"k"},
	})
	assert.Equal(t, "a", out.InstanceId)
	assert.Equal(t, "b", out.PeerInstanceId)
	assert.True(t, out.HelperAvailable && out.SelfCoordinationSupported && out.RemoteTriggerAllowed)
	assert.Equal(t, int64(5), out.SpoolFreeBytes)
	assert.Equal(t, "x", out.Error)
	assert.Equal(t, []string{"k"}, out.TrustedReleaseKeyIds)
}

func TestPackageToProtoCarriesTheKind(t *testing.T) {
	assert.Equal(t, pb.FrigatePackageKind_FRIGATE_PACKAGE_KIND_CONTROLLER_SOFTWARE, PackageToProto(domain.Package{Kind: domain.PackageKindFrigate}).Kind)
	assert.Equal(t, pb.FrigatePackageKind_FRIGATE_PACKAGE_KIND_PROTO_OS, PackageToProto(domain.Package{Kind: domain.PackageKindProtoOS}).Kind)
	assert.Equal(t, pb.FrigatePackageKind_FRIGATE_PACKAGE_KIND_UNSPECIFIED, PackageToProto(domain.Package{Kind: "weird"}).Kind)
}

func TestInstanceSupportToProtoCarriesProtoOSSupport(t *testing.T) {
	updated := time.Now().UTC().Truncate(time.Second)
	out := instanceSupportToProto(sdk.FrigateInstanceSupport{InstanceID: "a", ProtoOS: sdk.FrigateProtoOSSupport{
		SelfSupported: true, SupportMessage: "m", InstalledVersion: "0.6.1", ReleaseRepositories: []string{"block/proto-fleet"},
		Served: sdk.FrigateProtoOSBundle{Source: sdk.FrigateProtoOSBundlePackage, ReleaseTag: "v0.6.1"},
		Job: sdk.FrigateProtoOSJob{
			JobID: "job-1", ReleaseTag: "v0.6.2", TargetVersion: "0.6.2", State: sdk.FrigateProtoOSJobInstalling, Message: "Installing", UpdatedAt: updated,
		},
	}})
	require.NotNil(t, out.ProtoOs)
	assert.True(t, out.ProtoOs.SelfSupported)
	assert.Equal(t, "m", out.ProtoOs.SupportMessage)
	assert.Equal(t, "0.6.1", out.ProtoOs.InstalledVersion)
	assert.Equal(t, []string{"block/proto-fleet"}, out.ProtoOs.ReleaseRepositories)
	assert.Equal(t, pb.ProtoOSBundleSource_PROTO_OS_BUNDLE_SOURCE_PACKAGE, out.ProtoOs.ServedSource)
	assert.Equal(t, "v0.6.1", out.ProtoOs.ServedReleaseTag)
	require.NotNil(t, out.ProtoOs.Job)
	assert.Equal(t, "job-1", out.ProtoOs.Job.JobId)
	assert.Equal(t, "v0.6.2", out.ProtoOs.Job.ReleaseTag)
	assert.Equal(t, "0.6.2", out.ProtoOs.Job.TargetVersion)
	assert.Equal(t, pb.ProtoOSJobState_PROTO_OS_JOB_STATE_INSTALLING, out.ProtoOs.Job.State)
	assert.Equal(t, "Installing", out.ProtoOs.Job.Message)
	assert.Equal(t, updated, out.ProtoOs.Job.UpdatedAt.AsTime())

	noJob := instanceSupportToProto(sdk.FrigateInstanceSupport{InstanceID: "a", ProtoOS: sdk.FrigateProtoOSSupport{SelfSupported: true}})
	assert.Nil(t, noJob.ProtoOs.Job, "a controller that reports no job has none")
	assert.Nil(t, instanceSupportToProto(sdk.FrigateInstanceSupport{InstanceID: "a", Err: "down"}).ProtoOs, "an unread controller reports nothing")
	assert.Equal(t, pb.ProtoOSJobState_PROTO_OS_JOB_STATE_UNKNOWN, protoOSJobStateToProto("weird"))
	assert.Equal(t, pb.ProtoOSBundleSource_PROTO_OS_BUNDLE_SOURCE_UNKNOWN, bundleSourceToProto("weird"))
}

func TestJobToProtoCarriesProtoOSProgress(t *testing.T) {
	now := time.Now().UTC()
	out := jobToProto(domain.Job{
		Package: domain.Package{Kind: domain.PackageKindProtoOS, Version: "0.6.2"},
		Row:     sqlc.FrigateUpdateJob{ID: uuid.New(), State: domain.StateRunning, TargetVersion: "0.6.2", CreatedAt: now, UpdatedAt: now},
		Instances: []sqlc.FrigateUpdateInstance{{
			InstanceID: "a", StageState: domain.StageStaged, ProtoosJobState: string(sdk.FrigateProtoOSJobSucceeded),
			ProtoosInstalledVersion: "0.6.2", ProtoosServedReleaseTag: "v0.6.2", ErrorMessage: "proto-os 0.6.2 is installed",
		}},
	})
	assert.Equal(t, pb.FrigatePackageKind_FRIGATE_PACKAGE_KIND_PROTO_OS, out.Package.Kind)
	require.Len(t, out.Instances, 1)
	assert.Equal(t, pb.ProtoOSJobState_PROTO_OS_JOB_STATE_SUCCEEDED, out.Instances[0].ProtoOsJobState)
	assert.Equal(t, "0.6.2", out.Instances[0].ProtoOsInstalledVersion)
	assert.Equal(t, "v0.6.2", out.Instances[0].ProtoOsServedReleaseTag)
	assert.Equal(t, "proto-os 0.6.2 is installed", out.Instances[0].ErrorMessage)
}

func TestHandler_ProtoOSPackageIsListedAndRefusesStageOnly(t *testing.T) {
	e, _ := newProtoOSUploadEnv(t)
	_, uploaded := e.uploadBody("alice", e.protoOSBody())
	ctx := authn.SetInfo(t.Context(), e.alice)
	h := NewHandler(e.svc)

	listed, err := h.ListPackages(ctx, connect.NewRequest(&pb.ListPackagesRequest{}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.Packages, 1)
	assert.Equal(t, uploaded.Package.ID, listed.Msg.Packages[0].Id)
	assert.Equal(t, pb.FrigatePackageKind_FRIGATE_PACKAGE_KIND_PROTO_OS, listed.Msg.Packages[0].Kind)

	device := e.database.CreateDevice(e.alice.OrganizationID, "proto")
	_, err = e.database.DB.ExecContext(ctx, `UPDATE discovered_device SET device_class = 2 WHERE device_identifier = $1`, device.ID)
	require.NoError(t, err)
	_, err = e.database.DB.ExecContext(ctx, `INSERT INTO device_pairing (device_id, pairing_status) VALUES ($1, 'PAIRED')`, device.DatabaseID)
	require.NoError(t, err)
	_, err = h.StartUpdate(ctx, connect.NewRequest(&pb.StartUpdateRequest{
		DeviceIdentifier: device.ID, PackageId: uploaded.Package.ID, StageOnly: true, IdempotencyKey: "key-1",
	}))
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
}
