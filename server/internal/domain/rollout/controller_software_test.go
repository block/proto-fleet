package rollout

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

// addContainer adds a paired container reporting the controller software
// version. Its hashboards report no firmware yet.
func (f *fixture) addContainer(t *testing.T, identifier, controllerVersion string) int64 {
	t.Helper()
	id := f.addMiner(t, identifier, "CT1")
	_, err := f.conn.ExecContext(t.Context(),
		`UPDATE discovered_device SET device_class = 2, firmware_version = $2 WHERE org_id = $1 AND device_identifier = $3`,
		f.orgID, controllerVersion, identifier)
	require.NoError(t, err)
	return id
}

// uploadControllerSoftware records an uploaded controller software package.
func (f *fixture) uploadControllerSoftware(t *testing.T, orgID int64, version string) {
	t.Helper()
	var packages int
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM frigate_update_package`).Scan(&packages))
	_, err := f.conn.ExecContext(t.Context(), `
		INSERT INTO frigate_update_package (id, org_id, filename, version, architecture, sha256, size_bytes, repository, release_key_id, issued_at, statement, signature)
		VALUES ($1, $2, $3, $4, 'arm64', $5, 1, 'repo', 'key', now(), '\x00', '\x00')
	`, uuid.New(), orgID, "controller_"+version+".deb", version, fmt.Sprintf("%064x", packages+1))
	require.NoError(t, err)
}

func TestProtoOSPackageIsNotAControllerSoftwareVersion(t *testing.T) {
	f := newFixture(t, 0)
	f.addContainer(t, "ct1-a", "1.4.0")
	f.channel(t, allAtOnce, "ct1-a")
	_, err := f.conn.ExecContext(t.Context(), `
		INSERT INTO frigate_update_package (id, org_id, kind, filename, version, architecture, sha256, size_bytes, repository, release_key_id, issued_at, statement, signature)
		VALUES ($1, $2, 'proto_os', 'proto-os_v1.5.0_all.deb', '1.5.0', 'all', $3, 1, 'block/proto-fleet', 'key', now(), '\x00', '\x00')
	`, uuid.New(), f.orgID, fmt.Sprintf("%064x", 99))
	require.NoError(t, err)

	err = f.svc.SetControllerSoftware(t.Context(), f.orgID, testActor.ID, f.channelID, "Proto", "CT1", "1.5.0")

	require.True(t, fleeterror.IsFailedPreconditionError(err), "a ProtoOS package of the same version is not controller software: %v", err)
	f.uploadControllerSoftware(t, f.orgID, "1.5.0")
	require.NoError(t, f.svc.SetControllerSoftware(t.Context(), f.orgID, testActor.ID, f.channelID, "Proto", "CT1", "1.5.0"))
}

// hashboardFirmware makes fw-2 the container's hashboard firmware 2.0.0.
func (f *fixture) hashboardFirmware() Assignment {
	f.files.metadata = map[string]files.FirmwareMetadata{
		"fw-2": {TargetManufacturer: "Proto", TargetModel: "CT1", FirmwareVersion: "2.0.0", HashboardBuild: "dev"},
	}
	return Assignment{Manufacturer: "Proto", Model: "CT1", FirmwareFileID: "fw-2"}
}

func TestControllerSoftwareTargetMarksContainersBehind(t *testing.T) {
	f := newFixture(t, 1)
	f.addContainer(t, "ct1-a", "1.5.0")
	f.addContainer(t, "ct1-b", "1.4.0")
	f.channel(t, allAtOnce, "miner-0", "ct1-a", "ct1-b")
	ctx := t.Context()

	// Containers are listed before any target is set; miners never are.
	models, err := f.svc.ListControllerSoftware(ctx, f.orgID, f.channelID)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, ControllerSoftware{
		Manufacturer: "proto", Model: "CT1", ContainerCount: 2,
	}, models[0], "with no target a container is neither on nor behind it")

	// The target must name an uploaded package of this organization.
	err = f.svc.SetControllerSoftware(ctx, f.orgID, testActor.ID, f.channelID, "Proto", "CT1", "1.5.0")
	require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
	var otherOrg int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name) VALUES ('other-org', 'Other org') RETURNING id`).Scan(&otherOrg))
	f.uploadControllerSoftware(t, otherOrg, "1.5.0")
	err = f.svc.SetControllerSoftware(ctx, f.orgID, testActor.ID, f.channelID, "Proto", "CT1", "1.5.0")
	require.True(t, fleeterror.IsFailedPreconditionError(err), "another organization's package does not count: %v", err)
	require.Empty(t, f.activity.events)

	// Versions compare without a leading "v", and the pair folds case.
	f.uploadControllerSoftware(t, f.orgID, "v1.5.0")
	err = f.svc.SetControllerSoftware(ctx, f.orgID, testActor.ID, f.channelID, "Proto", "ct1", " 1.5.0 ")
	require.NoError(t, err)
	models, err = f.svc.ListControllerSoftware(ctx, f.orgID, f.channelID)
	require.NoError(t, err)
	require.Equal(t, "1.5.0", models[0].TargetVersion)
	require.EqualValues(t, 2, models[0].ContainerCount)
	require.EqualValues(t, 1, models[0].OnTargetCount)
	containers, cursor, err := f.svc.ListControllerSoftwareContainers(ctx, f.orgID, f.channelID, "proto", "CT1", 100, "")
	require.NoError(t, err)
	require.Empty(t, cursor)
	require.Equal(t, []ChannelContainer{
		{DeviceIdentifier: "ct1-a", ControllerSoftwareVersion: "1.5.0", OnTarget: true},
		{DeviceIdentifier: "ct1-b", ControllerSoftwareVersion: "1.4.0"},
	}, containers)
	require.Equal(t, []string{EventControllerSoftwareTargetChanged}, f.activity.types())
	require.Equal(t, "Set controller software version: Test channel Proto ct1 → 1.5.0", f.activity.events[0].Description)

	// Changing the target replaces it rather than adding a second one.
	f.uploadControllerSoftware(t, f.orgID, "1.6.0")
	err = f.svc.SetControllerSoftware(ctx, f.orgID, testActor.ID, f.channelID, "PROTO", "CT1", "1.6.0")
	require.NoError(t, err)
	models, err = f.svc.ListControllerSoftware(ctx, f.orgID, f.channelID)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "1.6.0", models[0].TargetVersion)
	require.Zero(t, models[0].OnTargetCount)

	// A container that reports no version is behind: it cannot prove the target.
	f.setReportedVersion(t, "ct1-a", "")
	f.setReportedVersion(t, "ct1-b", "V1.6.0")
	containers, _, err = f.svc.ListControllerSoftwareContainers(ctx, f.orgID, f.channelID, "proto", "CT1", 100, "")
	require.NoError(t, err)
	models, err = f.svc.ListControllerSoftware(ctx, f.orgID, f.channelID)
	require.NoError(t, err)
	require.EqualValues(t, 1, models[0].OnTargetCount)
	require.Equal(t, []ChannelContainer{
		{DeviceIdentifier: "ct1-a"},
		{DeviceIdentifier: "ct1-b", ControllerSoftwareVersion: "V1.6.0", OnTarget: true},
	}, containers)

	// Clearing returns the model to having no target.
	err = f.svc.SetControllerSoftware(ctx, f.orgID, testActor.ID, f.channelID, "Proto", "CT1", "")
	require.NoError(t, err)
	models, err = f.svc.ListControllerSoftware(ctx, f.orgID, f.channelID)
	require.NoError(t, err)
	require.Empty(t, models[0].TargetVersion)
	require.Zero(t, models[0].OnTargetCount)
	require.EqualValues(t, 2, models[0].ContainerCount)
	require.Equal(t, "Cleared controller software version: Test channel Proto CT1", f.activity.events[len(f.activity.events)-1].Description)

	// Clearing again changes nothing and records nothing.
	recorded := len(f.activity.events)
	err = f.svc.SetControllerSoftware(ctx, f.orgID, testActor.ID, f.channelID, "Proto", "CT1", "")
	require.NoError(t, err)
	require.Len(t, f.activity.events, recorded)
}

func TestControllerSoftwareTargetIsScopedToItsChannel(t *testing.T) {
	f := newFixture(t, 0)
	f.addContainer(t, "ct1-a", "1.4.0")
	f.channel(t, allAtOnce, "ct1-a")
	f.uploadControllerSoftware(t, f.orgID, "1.5.0")
	ctx := t.Context()

	_, err := f.svc.ListControllerSoftware(ctx, f.orgID+1, f.channelID)
	require.True(t, fleeterror.IsNotFoundError(err), "%v", err)
	err = f.svc.SetControllerSoftware(ctx, f.orgID+1, testActor.ID, f.channelID, "Proto", "CT1", "1.5.0")
	require.True(t, fleeterror.IsNotFoundError(err), "%v", err)
	err = f.svc.SetControllerSoftware(ctx, f.orgID, testActor.ID, f.channelID, " ", "CT1", "1.5.0")
	require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)

	// A target for a model with no containers yet is kept and listed.
	err = f.svc.SetControllerSoftware(ctx, f.orgID, testActor.ID, f.channelID, "Proto", "CT2", "1.5.0")
	require.NoError(t, err)
	models, err := f.svc.ListControllerSoftware(ctx, f.orgID, f.channelID)
	require.NoError(t, err)
	require.Len(t, models, 2)
	require.Equal(t, ControllerSoftware{Manufacturer: "Proto", Model: "CT2", TargetVersion: "1.5.0"}, models[0])
	require.Empty(t, models[1].TargetVersion, "CT1 has no target")

	// Deleting the channel deletes its targets.
	require.NoError(t, f.svc.DeleteChannel(ctx, f.orgID, f.channelID))
	var remaining int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM release_channel_controller_software`).Scan(&remaining))
	require.Zero(t, remaining)
}

// The controller installs hashboard firmware, so a container behind the
// channel's controller software version takes no firmware until an operator
// updates it. Waiting is not a failed attempt.
func TestRolloutHoldsHashboardFirmwareUntilControllerSoftwareMatches(t *testing.T) {
	f := newFixture(t, 0)
	f.addContainer(t, "ct1-a", "1.5.0")
	f.addContainer(t, "ct1-b", "1.4.0")
	f.channel(t, allAtOnce, "ct1-a", "ct1-b")
	f.uploadControllerSoftware(t, f.orgID, "1.5.0")
	ctx := t.Context()
	err := f.svc.SetControllerSoftware(ctx, f.orgID, testActor.ID, f.channelID, "Proto", "CT1", "1.5.0")
	require.NoError(t, err)

	started, err := f.svc.ApplyFirmware(ctx, f.orgID, testActor, f.channelID, []Assignment{f.hashboardFirmware()}, nil)
	require.NoError(t, err)
	require.Len(t, started, 1)
	rolloutID := started[0].ID

	f.svc.EnforceTick(ctx)
	require.Equal(t, []string{"ct1-a"}, f.dispatcher.sentIdentifiers(), "only the container on the target is updated")
	const waiting = "Waiting for controller software 1.5.0 on 1 container. Update its controller software to continue."
	require.Equal(t, waiting, f.rollout(t, rolloutID).Evidence.HoldReason, "nothing else is due, so the rollout says what it waits for")

	// The rollout stays open for the held container after the other verifies.
	q := sqlc.New(f.conn)
	_, err = q.AdvanceContainerInventory(ctx, sqlc.AdvanceContainerInventoryParams{DeviceIdentifier: "ct1-a", ObservedAt: time.Now(), HashboardFirmwareVersion: "2.0.0"})
	require.NoError(t, err)
	f.svc.EnforceTick(ctx)
	require.Equal(t, PhaseDone, phaseOf(f.rollout(t, rolloutID), "ct1-a"))

	// The held container waits past every resend interval without using an attempt.
	for range MaxAttempts + 1 {
		f.advanceClock(time.Hour)
		f.backdateSends(t)
		f.svc.EnforceTick(ctx)
	}
	r := f.rollout(t, rolloutID)
	held := deviceOf(t, r, "ct1-b")
	require.Equal(t, PhaseQueued, held.Phase)
	require.Zero(t, held.Attempts)
	require.Nil(t, held.LastSentAt)
	require.Equal(t, []string{"ct1-a"}, f.dispatcher.sentIdentifiers())
	require.Equal(t, StatusActive, r.Status, "the rollout waits for the held container")
	require.Equal(t, waiting, r.Evidence.HoldReason)

	// Updating the controller software releases the hold on the next tick.
	f.setReportedVersion(t, "ct1-b", "1.5.0")
	f.svc.EnforceTick(ctx)
	require.Equal(t, []string{"ct1-a", "ct1-b"}, f.dispatcher.sentIdentifiers())
	r = f.rollout(t, rolloutID)
	require.Equal(t, PhaseInProgress, phaseOf(r, "ct1-b"))
	require.Empty(t, r.Evidence.HoldReason)
}

// Clearing the target releases the hold, and a channel without a target never
// holds.
func TestClearingControllerSoftwareTargetReleasesHold(t *testing.T) {
	f := newFixture(t, 0)
	f.addContainer(t, "ct1-a", "1.4.0")
	f.channel(t, allAtOnce, "ct1-a")
	f.uploadControllerSoftware(t, f.orgID, "1.5.0")
	ctx := t.Context()
	err := f.svc.SetControllerSoftware(ctx, f.orgID, testActor.ID, f.channelID, "Proto", "CT1", "1.5.0")
	require.NoError(t, err)
	_, err = f.svc.ApplyFirmware(ctx, f.orgID, testActor, f.channelID, []Assignment{f.hashboardFirmware()}, nil)
	require.NoError(t, err)

	f.svc.EnforceTick(ctx)
	require.Empty(t, f.dispatcher.sent)

	err = f.svc.SetControllerSoftware(ctx, f.orgID, testActor.ID, f.channelID, "Proto", "CT1", "")
	require.NoError(t, err)
	f.svc.EnforceTick(ctx)
	require.Equal(t, []string{"ct1-a"}, f.dispatcher.sentIdentifiers())
}

// A delegated controller cannot advance a held container; the rejection names
// it and queues nothing, so the controller can skip it or wait.
func TestDelegatedAdvanceRejectsContainersBehindControllerSoftware(t *testing.T) {
	f := newFixture(t, 0)
	f.addContainer(t, "ct1-a", "1.5.0")
	f.addContainer(t, "ct1-b", "1.4.0")
	f.channel(t, delegated(), "ct1-a", "ct1-b")
	f.uploadControllerSoftware(t, f.orgID, "1.5.0")
	ctx := t.Context()
	err := f.svc.SetControllerSoftware(ctx, f.orgID, testActor.ID, f.channelID, "Proto", "CT1", "1.5.0")
	require.NoError(t, err)
	started, err := f.svc.ApplyFirmware(ctx, f.orgID, testActor, f.channelID, []Assignment{f.hashboardFirmware()}, nil)
	require.NoError(t, err)
	require.Len(t, started, 1)
	r := started[0]

	_, _, err = f.svc.AdvanceRollout(ctx, f.orgID, r.ID, DeviceSelection{DeviceIdentifiers: []string{"ct1-a", "ct1-b"}}, byOperator)
	requireReason(t, err, ReasonDeviceNotDispatchable)
	info, _ := ReasonOf(err)
	require.Equal(t, []string{"ct1-b"}, info.DeviceIdentifiers)
	require.Empty(t, f.dispatcher.sent, "the whole selection is rejected")
	require.Equal(t, r.Revision, f.rollout(t, r.ID).Revision)

	_, sent, err := f.svc.AdvanceRollout(ctx, f.orgID, r.ID, DeviceSelection{DeviceIdentifiers: []string{"ct1-a"}}, byOperator)
	require.NoError(t, err)
	require.Equal(t, []string{"ct1-a"}, sent)
}

func TestControllerSoftwareContainersPageBeyond500(t *testing.T) {
	f := newFixture(t, 0)
	identifiers := make([]string, 501)
	for i := range identifiers {
		identifiers[i] = fmt.Sprintf("ct1-%04d", i)
		version := "1.6.0"
		if i == 500 {
			version = "1.5.0"
		}
		f.addContainer(t, identifiers[i], version)
	}
	f.channel(t, allAtOnce, identifiers...)
	f.uploadControllerSoftware(t, f.orgID, "1.6.0")
	ctx := t.Context()
	require.NoError(t, f.svc.SetControllerSoftware(ctx, f.orgID, testActor.ID, f.channelID, "Proto", "CT1", "1.6.0"))
	summary, err := f.svc.ListControllerSoftware(ctx, f.orgID, f.channelID)
	require.NoError(t, err)
	require.Equal(t, []ControllerSoftware{{Manufacturer: "proto", Model: "CT1", TargetVersion: "1.6.0", ContainerCount: 501, OnTargetCount: 500}}, summary)
	page, cursor, err := f.svc.ListControllerSoftwareContainers(ctx, f.orgID, f.channelID, "proto", "CT1", 500, "")
	require.NoError(t, err)
	require.Len(t, page, 500)
	require.NotEmpty(t, cursor)
	require.Equal(t, identifiers[499], page[499].DeviceIdentifier)
	last, next, err := f.svc.ListControllerSoftwareContainers(ctx, f.orgID, f.channelID, "proto", "CT1", 500, cursor)
	require.NoError(t, err)
	require.Empty(t, next)
	require.Equal(t, []ChannelContainer{{DeviceIdentifier: identifiers[500], ControllerSoftwareVersion: "1.5.0"}}, last)
	for _, invalid := range []string{"garbage", encodeCursor("ct1", "-1"), encodeCursor("ct1", "bad")} {
		_, _, err = f.svc.ListControllerSoftwareContainers(ctx, f.orgID, f.channelID, "proto", "CT1", 500, invalid)
		require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	}
	_, _, err = f.svc.ListControllerSoftwareContainers(ctx, f.orgID+1, f.channelID, "proto", "CT1", 500, "")
	require.True(t, fleeterror.IsNotFoundError(err), "%v", err)
	empty, _, err := f.svc.ListControllerSoftwareContainers(ctx, f.orgID, f.channelID, "Proto", "CT1", 500, "")
	require.NoError(t, err)
	require.Empty(t, empty, "details match the observed pair verbatim")
}

func TestControllerSoftwareSummaryPreservesObservedPairs(t *testing.T) {
	f := newFixture(t, 0)
	f.addContainer(t, "ct1-a", "v1.6.0")
	f.addContainer(t, "ct1-b", "1.5.0")
	f.addContainer(t, "ct1-c", "")
	f.addMiner(t, "miner", "CT1")
	f.channel(t, allAtOnce, "ct1-a", "ct1-b", "ct1-c", "miner")
	f.uploadControllerSoftware(t, f.orgID, "1.6.0")
	ctx := t.Context()
	_, err := f.conn.ExecContext(ctx, `UPDATE discovered_device SET manufacturer = 'Proto' WHERE org_id = $1 AND device_identifier = 'ct1-b'`, f.orgID)
	require.NoError(t, err)
	require.NoError(t, f.svc.SetControllerSoftware(ctx, f.orgID, testActor.ID, f.channelID, "PROTO", "ct1", "1.6.0"))
	summary, err := f.svc.ListControllerSoftware(ctx, f.orgID, f.channelID)
	require.NoError(t, err)
	require.Equal(t, []ControllerSoftware{
		{Manufacturer: "Proto", Model: "CT1", TargetVersion: "1.6.0", ContainerCount: 1},
		{Manufacturer: "proto", Model: "CT1", TargetVersion: "1.6.0", ContainerCount: 2, OnTargetCount: 1},
	}, summary)
	// Null and empty observed identities represent one summary pair.
	_, err = f.conn.ExecContext(ctx, `UPDATE discovered_device SET manufacturer = CASE WHEN device_identifier = 'ct1-a' THEN NULL ELSE '' END WHERE org_id = $1 AND device_identifier IN ('ct1-a', 'ct1-b')`, f.orgID)
	require.NoError(t, err)
	summary, err = f.svc.ListControllerSoftware(ctx, f.orgID, f.channelID)
	require.NoError(t, err)
	require.EqualValues(t, 2, summary[0].ContainerCount)
	require.Empty(t, summary[0].Manufacturer)
	containers, _, err := f.svc.ListControllerSoftwareContainers(ctx, f.orgID, f.channelID, "", "CT1", 100, "")
	require.NoError(t, err)
	require.Len(t, containers, 2, "an empty observed identity is an exact filter")
}
