package rollout

import (
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

const (
	psuCapsuleChecksum = "3333333333333333333333333333333333333333333333333333333333333333"
	// What BOCO PSUs report: before the capsule, and once it is applied.
	psuInstalledVersion = "app=1310;bl=1110"
	psuCapsuleVersion   = "app=1410;bl=1110"
)

var (
	ct1Hashboards = Assignment{Manufacturer: "Proto", Model: "CT1", FirmwareFileID: "fw-2"}
	ct1PSU        = Assignment{Manufacturer: "Proto", Model: "CT1", Component: ComponentPSU, FirmwareFileID: "psu-1"}
)

// ct1Channel is a channel holding one CT1 container, with a hashboard archive
// (fw-2, 2.0.0) and a PSU capsule (psu-1) uploaded for it.
type ct1Channel struct {
	*fixture
	deviceID int64
}

func newCT1Channel(t *testing.T) *ct1Channel {
	t.Helper()
	f := newFixture(t, 0)
	c := &ct1Channel{fixture: f, deviceID: f.addMiner(t, "ct1", "CT1")}
	_, err := f.conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2,firmware_version='9.9.9' WHERE device_identifier='ct1'`)
	require.NoError(t, err)
	f.files.artifacts = maps.Clone(f.files.artifacts)
	f.files.artifacts["psu-1"] = files.FirmwareArtifact{FileID: "psu-1", Checksum: psuCapsuleChecksum, Metadata: files.FirmwareMetadata{
		TargetManufacturer: "Proto", TargetModel: "CT1", FirmwareVersion: psuCapsuleVersion, Component: files.FirmwareComponentPSU,
	}}
	f.files.metadata = map[string]files.FirmwareMetadata{
		"fw-2": {TargetManufacturer: "Proto", TargetModel: "CT1", FirmwareVersion: "2.0.0", HashboardBuild: "dev"},
	}
	f.channel(t, allAtOnce, "ct1")
	return c
}

// observe stores a fresh inventory: hashboards on 2.0.0 and the given PSU
// firmware evidence.
func (c *ct1Channel) observe(t *testing.T, psuVersion string, psuMixed bool) {
	t.Helper()
	_, err := sqlc.New(c.conn).AdvanceContainerInventory(t.Context(), sqlc.AdvanceContainerInventoryParams{
		DeviceIdentifier: "ct1", ObservedAt: time.Now(), HashboardFirmwareVersion: "2.0.0",
		PsuFirmwareVersion: psuVersion, PsuFirmwareMixed: psuMixed,
	})
	require.NoError(t, err)
}

func (c *ct1Channel) apply(t *testing.T, assignments ...Assignment) []Rollout {
	t.Helper()
	started, err := c.svc.ApplyFirmware(t.Context(), c.orgID, testActor, c.channelID, assignments, nil)
	require.NoError(t, err)
	return started
}

// groups returns the channel's model groups by component.
func (c *ct1Channel) groups(t *testing.T) map[string]ModelGroup {
	t.Helper()
	groups, _, err := c.svc.ListChannelModelGroups(t.Context(), c.orgID, c.channelID, 100, "")
	require.NoError(t, err)
	out := map[string]ModelGroup{}
	for _, g := range groups {
		require.True(t, strings.EqualFold("Proto", g.Manufacturer) && g.Model == "CT1", "%s %s", g.Manufacturer, g.Model)
		out[g.Component] = g
	}
	require.Len(t, out, len(groups), "one group per component")
	return out
}

// deployed returns the artifact Fleet recorded for each component.
func (c *ct1Channel) deployed(t *testing.T) map[string]string {
	t.Helper()
	rows, err := c.conn.QueryContext(t.Context(), `SELECT component, firmware_checksum FROM device_firmware_deployment WHERE device_id = $1`, c.deviceID)
	require.NoError(t, err)
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var component, checksum string
		require.NoError(t, rows.Scan(&component, &checksum))
		out[component] = checksum
	}
	require.NoError(t, rows.Err())
	return out
}

// sentPSU counts the dispatches that carried the PSU capsule; the rest carried
// the hashboard archive.
func (c *ct1Channel) sent() (hashboards, psu int) {
	for _, artifact := range c.dispatcher.artifacts {
		if artifact.Metadata.IsPSU() {
			psu++
		} else {
			hashboards++
		}
	}
	return hashboards, psu
}

// hashboardsOnTarget assigns the hashboard firmware and lets it converge.
func (c *ct1Channel) hashboardsOnTarget(t *testing.T) {
	t.Helper()
	started := c.apply(t, ct1Hashboards)
	require.Len(t, started, 1)
	require.Equal(t, ComponentMain, started[0].Component)
	c.svc.EnforceTick(t.Context())
	c.observe(t, psuInstalledVersion, false)
	c.svc.EnforceTick(t.Context())
	require.Equal(t, PhaseDone, phaseOf(c.rollout(t, started[0].ID), "ct1"))
	hashboards, psu := c.sent()
	require.Equal(t, 1, hashboards)
	require.Zero(t, psu)
}

// A CT1 container has two firmware components. A channel assigns, rolls out
// and verifies each on its own: PSU firmware against the PSU version the
// container reports, hashboard firmware against its hashboard version.
func TestCT1PSUFirmwareIsAssignedBesideHashboardFirmware(t *testing.T) {
	c := newCT1Channel(t)
	ctx := t.Context()
	c.hashboardsOnTarget(t)

	// A file backs only the assignment of the component it is firmware for.
	_, err := c.svc.PreviewFirmware(ctx, c.orgID, c.channelID, []Assignment{{Manufacturer: "Proto", Model: "CT1", FirmwareFileID: "psu-1"}}, nil)
	requireReason(t, err, ReasonArtifactMismatch)
	require.ErrorContains(t, err, "is PSU firmware, not main firmware")
	_, err = c.svc.ApplyFirmware(ctx, c.orgID, testActor, c.channelID, []Assignment{{Manufacturer: "Proto", Model: "CT1", Component: ComponentPSU, FirmwareFileID: "fw-2"}}, nil)
	requireReason(t, err, ReasonArtifactMismatch)
	require.ErrorContains(t, err, "is main firmware, not PSU firmware")
	_, err = c.svc.PreviewFirmware(ctx, c.orgID, c.channelID, []Assignment{{Manufacturer: "Proto", Model: "CT1", Component: "fan", FirmwareFileID: "psu-1"}}, nil)
	require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	_, err = c.svc.PreviewFirmware(ctx, c.orgID, c.channelID, []Assignment{ct1PSU, ct1PSU}, nil)
	require.ErrorContains(t, err, "Proto CT1 PSU more than once")

	plans, err := c.svc.PreviewFirmware(ctx, c.orgID, c.channelID, []Assignment{ct1Hashboards, ct1PSU}, nil)
	require.NoError(t, err)
	require.Len(t, plans, 2)
	require.True(t, plans[0].Unchanged)
	require.Equal(t, PairKey{Manufacturer: "Proto", Model: "CT1", Component: ComponentPSU}, plans[1].Pair)
	require.EqualValues(t, 1, plans[1].TargetCount)
	require.Zero(t, plans[1].OnTargetCount)

	// Assigning the capsule starts a PSU rollout and leaves the hashboard
	// assignment and its provenance alone.
	started := c.apply(t, ct1Hashboards, ct1PSU)
	require.Len(t, started, 1, "the unchanged hashboard assignment starts nothing")
	psuRollout := started[0]
	require.Equal(t, ComponentPSU, psuRollout.Component)
	require.Equal(t, psuCapsuleVersion, psuRollout.FirmwareVersion)
	groups := c.groups(t)
	require.Len(t, groups, 2)
	require.Equal(t, checksum2, groups[ComponentMain].FirmwareChecksum)
	require.EqualValues(t, 1, groups[ComponentMain].OnTargetCount)
	require.Equal(t, []string{"2.0.0"}, groups[ComponentMain].ReportedVersions)
	require.Zero(t, groups[ComponentMain].ActiveRolloutID)
	require.Equal(t, psuCapsuleChecksum, groups[ComponentPSU].FirmwareChecksum)
	require.Equal(t, psuCapsuleVersion, groups[ComponentPSU].FirmwareVersion)
	require.Zero(t, groups[ComponentPSU].OnTargetCount)
	require.EqualValues(t, 1, groups[ComponentPSU].MinerCount)
	require.Equal(t, []string{psuInstalledVersion}, groups[ComponentPSU].ReportedVersions)
	require.Equal(t, psuRollout.ID, groups[ComponentPSU].ActiveRolloutID)
	channel, err := c.svc.GetChannel(ctx, c.orgID, c.channelID)
	require.NoError(t, err)
	require.EqualValues(t, 2, channel.ModelGroupCount)

	c.svc.EnforceTick(ctx)
	hashboards, psu := c.sent()
	require.Equal(t, 1, hashboards, "the hashboard firmware is not sent again")
	require.Equal(t, 1, psu)
	sent := c.dispatcher.artifacts[len(c.dispatcher.artifacts)-1]
	require.Equal(t, psuCapsuleChecksum, sent.Checksum)
	require.Equal(t, psuCapsuleVersion, sent.Metadata.FirmwareVersion)

	// The hashboard version cannot verify a PSU update.
	c.observe(t, psuInstalledVersion, false)
	c.svc.EnforceTick(ctx)
	require.Equal(t, PhaseInProgress, phaseOf(c.rollout(t, psuRollout.ID), "ct1"))
	require.Equal(t, psuInstalledVersion, deviceOf(t, c.rollout(t, psuRollout.ID), "ct1").FirmwareVersion)
	// Neither can PSUs that disagree, or a version nobody observed.
	c.observe(t, "", true)
	c.svc.EnforceTick(ctx)
	require.Equal(t, VersionStateMixed, deviceOf(t, c.rollout(t, psuRollout.ID), "ct1").FirmwareVersionState)
	c.observe(t, "", false)
	c.svc.EnforceTick(ctx)
	require.Equal(t, VersionStateUnknown, deviceOf(t, c.rollout(t, psuRollout.ID), "ct1").FirmwareVersionState)
	require.Equal(t, PhaseInProgress, phaseOf(c.rollout(t, psuRollout.ID), "ct1"))
	require.Equal(t, map[string]string{ComponentMain: checksum2, ComponentPSU: ""}, c.deployed(t), "the capsule's command finished; nothing verified it yet")

	c.observe(t, psuCapsuleVersion, false)
	c.svc.EnforceTick(ctx)
	require.Equal(t, PhaseDone, phaseOf(c.rollout(t, psuRollout.ID), "ct1"))
	require.Equal(t, map[string]string{ComponentMain: checksum2, ComponentPSU: psuCapsuleChecksum}, c.deployed(t))
	groups = c.groups(t)
	require.EqualValues(t, 1, groups[ComponentMain].OnTargetCount)
	require.EqualValues(t, 1, groups[ComponentPSU].OnTargetCount)

	// Members are read per component.
	for component, want := range map[string][2]string{
		ComponentMain: {"2.0.0", checksum2},
		ComponentPSU:  {psuCapsuleVersion, psuCapsuleChecksum},
	} {
		miners, _, err := c.svc.ListChannelMiners(ctx, c.orgID, c.channelID, "", "", component, 10, "")
		require.NoError(t, err)
		require.Len(t, miners, 1)
		require.Equal(t, want[0], miners[0].FirmwareVersion)
		require.Equal(t, want[1], miners[0].LastDeployedFirmwareChecksum)
	}
	_, _, err = c.svc.ListChannelMiners(ctx, c.orgID, c.channelID, "", "", "fan", 10, "")
	require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)

	// PSUs that drift are brought back with the capsule; hashboards that did
	// not drift are left alone.
	c.observe(t, "", true)
	c.svc.EnforceTick(ctx)
	c.svc.EnforceTick(ctx)
	hashboards, psu = c.sent()
	require.Equal(t, 1, hashboards)
	require.Equal(t, 2, psu)
	c.observe(t, psuCapsuleVersion, false)
	c.svc.EnforceTick(ctx)
	require.EqualValues(t, 1, c.groups(t)[ComponentPSU].OnTargetCount)

	// Clearing one component's assignment leaves the other's.
	require.Empty(t, c.apply(t, Assignment{Manufacturer: "Proto", Model: "CT1", Component: ComponentPSU}))
	groups = c.groups(t)
	require.Len(t, groups, 1, "an unassigned PSU component is not a group")
	require.Equal(t, checksum2, groups[ComponentMain].FirmwareChecksum)
	require.EqualValues(t, 1, groups[ComponentMain].OnTargetCount)
	c.observe(t, psuInstalledVersion, false)
	c.svc.EnforceTick(ctx)
	hashboards, psu = c.sent()
	require.Equal(t, 1, hashboards)
	require.Equal(t, 2, psu, "nothing enforces a cleared assignment")
}

// A channel owns a container's firmware per component, so a direct update of
// a component the channel does not assign is not the channel's to refuse.
func TestDirectFirmwareCommandsAreOwnedPerComponent(t *testing.T) {
	c := newCT1Channel(t)
	ctx := t.Context()
	filter := command.NewReleaseChannelFirmwareFilter(c.conn)
	direct := func(component string) command.CommandFilterOutput {
		t.Helper()
		out, err := filter.Apply(ctx, command.CommandFilterInput{
			CommandType: commandtype.FirmwareUpdate, OrganizationID: c.orgID, DeviceIdentifiers: []string{"ct1"}, FirmwareComponent: component,
		})
		require.NoError(t, err)
		return out
	}
	requireOwned := func(component string, owned bool) {
		t.Helper()
		out := direct(component)
		if !owned {
			require.Equal(t, []string{"ct1"}, out.Kept)
			require.Empty(t, out.Skipped)
			return
		}
		require.Empty(t, out.Kept)
		require.Len(t, out.Skipped, 1)
		require.Contains(t, out.Skipped[0].Reason, "Test channel")
	}

	requireOwned(ComponentMain, false)
	requireOwned(ComponentPSU, false)

	c.hashboardsOnTarget(t)
	requireOwned(ComponentMain, true)
	requireOwned(ComponentPSU, false)

	c.apply(t, ct1PSU)
	requireOwned(ComponentMain, true)
	requireOwned(ComponentPSU, true)

	c.apply(t, Assignment{Manufacturer: "Proto", Model: "CT1"})
	requireOwned(ComponentMain, false)
	requireOwned(ComponentPSU, true)
}

// A finished firmware command invalidates what Fleet knows it deployed, for
// the component the command updated only. A direct PSU update on a container
// whose channel assigns hashboard firmware must not make the channel send the
// hashboard firmware again.
func TestDirectPSUUpdateLeavesHashboardProvenance(t *testing.T) {
	c := newCT1Channel(t)
	ctx := t.Context()
	c.hashboardsOnTarget(t)
	finish := func(payload map[string]string) {
		t.Helper()
		uuid := createProvenanceCommand(t, c.fixture, payload, false)
		require.NoError(t, sqlc.New(c.conn).UpsertCommandOnDeviceLog(ctx, sqlc.UpsertCommandOnDeviceLogParams{
			Uuid: uuid, DeviceID: c.deviceID, Status: sqlc.DeviceCommandStatusEnumSUCCESS, UpdatedAt: time.Now(),
		}))
	}

	// While the direct PSU update runs, the hashboard assignment still matches.
	_, err := c.conn.ExecContext(ctx, `
		INSERT INTO queue_message (command_batch_log_uuid, device_id, command_type, status, retry_count, payload)
		VALUES ('00000000-0000-0000-0000-000000000000', $1, 'FirmwareUpdate', 'PROCESSING', 0,
		        jsonb_build_object('firmware_file_id', 'psu-1', 'firmware_component', 'psu'))`, c.deviceID)
	require.NoError(t, err)
	c.observe(t, psuInstalledVersion, false)
	c.svc.EnforceTick(ctx)
	require.EqualValues(t, 1, c.groups(t)[ComponentMain].OnTargetCount)
	hashboards, _ := c.sent()
	require.Equal(t, 1, hashboards)
	var rollouts int
	require.NoError(t, c.conn.QueryRowContext(ctx, `SELECT count(*) FROM firmware_rollout WHERE channel_id = $1`, c.channelID).Scan(&rollouts))
	require.Equal(t, 1, rollouts, "an update of another component must not open a hashboard rollout")

	// It finishes: only PSU provenance is invalidated.
	_, err = c.conn.ExecContext(ctx, `DELETE FROM queue_message WHERE device_id = $1`, c.deviceID)
	require.NoError(t, err)
	finish(map[string]string{"firmware_file_id": "psu-1", "firmware_component": "psu"})
	require.Equal(t, map[string]string{ComponentMain: checksum2, ComponentPSU: ""}, c.deployed(t))
	c.observe(t, psuCapsuleVersion, false)
	c.svc.EnforceTick(ctx)
	hashboards, psu := c.sent()
	require.Equal(t, 1, hashboards, "a direct PSU update must not make the channel send hashboard firmware again")
	require.Zero(t, psu)
	require.EqualValues(t, 1, c.groups(t)[ComponentMain].OnTargetCount)

	// A direct update of the hashboard firmware, or one that names no
	// component, still invalidates the hashboard provenance.
	finish(map[string]string{"firmware_file_id": "manual", "firmware_checksum": strings.Repeat("ab", 32)})
	require.Equal(t, map[string]string{ComponentMain: "", ComponentPSU: ""}, c.deployed(t))
	c.observe(t, psuCapsuleVersion, false)
	c.svc.EnforceTick(ctx)
	c.svc.EnforceTick(ctx)
	hashboards, psu = c.sent()
	require.Equal(t, 2, hashboards)
	require.Zero(t, psu)
}

// A container takes one firmware command at a time. While a command for one
// component is on its queue, a rollout of the other component sends nothing
// and uses no attempt. It dispatches once the queue drains.
func TestPendingCommandOfAnotherComponentHoldsDispatch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pending string
		assign  Assignment
	}{
		{"a PSU update holds hashboard firmware", `{"firmware_file_id":"psu-1","firmware_component":"psu"}`, ct1Hashboards},
		// A command that names no component updates the main firmware.
		{"a hashboard update holds PSU firmware", `{"firmware_file_id":"manual"}`, ct1PSU},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCT1Channel(t)
			ctx := t.Context()
			c.observe(t, psuInstalledVersion, false)
			_, err := c.conn.ExecContext(ctx, `
				INSERT INTO queue_message (command_batch_log_uuid, device_id, command_type, status, retry_count, payload)
				VALUES ('00000000-0000-0000-0000-000000000000', $1, 'FirmwareUpdate', 'PROCESSING', 0, $2::jsonb)`, c.deviceID, tc.pending)
			require.NoError(t, err)

			started := c.apply(t, tc.assign)
			require.Len(t, started, 1)
			require.Equal(t, tc.assign.Component, started[0].Component)
			c.svc.EnforceTick(ctx)
			c.svc.EnforceTick(ctx)
			require.Empty(t, c.dispatcher.artifacts, "a container with a firmware command on its queue takes no other")
			held := deviceOf(t, c.rollout(t, started[0].ID), "ct1")
			require.Zero(t, held.Attempts)
			require.Equal(t, PhaseQueued, held.Phase)

			_, err = c.conn.ExecContext(ctx, `DELETE FROM queue_message WHERE device_id = $1`, c.deviceID)
			require.NoError(t, err)
			c.svc.EnforceTick(ctx)
			require.Len(t, c.dispatcher.artifacts, 1)
			require.Equal(t, tc.assign.Component, c.dispatcher.artifacts[0].Metadata.ComponentKey())
			require.Equal(t, PhaseInProgress, phaseOf(c.rollout(t, started[0].ID), "ct1"))
		})
	}
}

// A container's controllers install every firmware a channel sends it, so the
// channel's controller software target holds PSU firmware as it holds
// hashboard firmware.
func TestControllerSoftwareTargetHoldsCT1PSUFirmware(t *testing.T) {
	c := newCT1Channel(t)
	ctx := t.Context()
	c.hashboardsOnTarget(t)
	// The container reports controller software 9.9.9.
	c.uploadControllerSoftware(t, c.orgID, "10.0.0")
	require.NoError(t, c.svc.SetControllerSoftware(ctx, c.orgID, testActor.ID, c.channelID, "Proto", "CT1", "10.0.0"))

	started := c.apply(t, ct1PSU)
	require.Len(t, started, 1)
	c.svc.EnforceTick(ctx)
	_, psu := c.sent()
	require.Zero(t, psu, "a container behind the target takes no PSU firmware")
	r := c.rollout(t, started[0].ID)
	held := deviceOf(t, r, "ct1")
	require.Equal(t, PhaseQueued, held.Phase)
	require.Zero(t, held.Attempts)
	require.Equal(t, "Waiting for controller software 10.0.0 on 1 container. Update its controller software to continue.", r.Evidence.HoldReason)

	c.setReportedVersion(t, "ct1", "10.0.0")
	c.svc.EnforceTick(ctx)
	_, psu = c.sent()
	require.Equal(t, 1, psu)
	require.Equal(t, PhaseInProgress, phaseOf(c.rollout(t, started[0].ID), "ct1"))
}

// Rollback acts on one component of a model. Rolling back a PSU rollout
// restores or clears the PSU assignment and leaves the hashboard assignment,
// its rollout, and the container's hashboard firmware alone.
func TestCT1PSURollbackLeavesHashboardFirmware(t *testing.T) {
	const laterCapsuleChecksum = "4444444444444444444444444444444444444444444444444444444444444444"
	c := newCT1Channel(t)
	ctx := t.Context()
	c.files.artifacts["psu-2"] = files.FirmwareArtifact{FileID: "psu-2", Checksum: laterCapsuleChecksum, Metadata: files.FirmwareMetadata{
		TargetManufacturer: "Proto", TargetModel: "CT1", FirmwareVersion: "app=1510;bl=1110", Component: files.FirmwareComponentPSU,
	}}
	c.hashboardsOnTarget(t)
	requireHashboardsUntouched := func() {
		t.Helper()
		group := c.groups(t)[ComponentMain]
		require.Equal(t, checksum2, group.FirmwareChecksum)
		require.EqualValues(t, 1, group.OnTargetCount)
		require.Zero(t, group.ActiveRolloutID, "a PSU rollback must not start a hashboard rollout")
		hashboards, _ := c.sent()
		require.Equal(t, 1, hashboards, "a PSU rollback must not send hashboard firmware again")
	}

	// The first PSU assignment replaced nothing, so its rollback clears it.
	first := c.apply(t, ct1PSU)[0]
	_, restored, err := c.svc.RollbackFirmware(ctx, c.orgID, first.ID, byOperator)
	require.NoError(t, err)
	require.Empty(t, restored)
	require.Len(t, c.groups(t), 1, "the PSU assignment is cleared")
	canceled := c.rollout(t, first.ID)
	require.Equal(t, StatusCanceled, canceled.Status)
	require.Equal(t, CancelReasonRolledBack, canceled.CancelReason)
	c.svc.EnforceTick(ctx)
	_, psu := c.sent()
	require.Zero(t, psu)
	requireHashboardsUntouched()

	// A capsule that replaced another rolls back to the capsule it replaced.
	c.apply(t, ct1PSU)
	later := c.apply(t, Assignment{Manufacturer: "Proto", Model: "CT1", Component: ComponentPSU, FirmwareFileID: "psu-2"})[0]
	require.Equal(t, psuCapsuleChecksum, later.PreviousFirmwareChecksum)
	_, restored, err = c.svc.RollbackFirmware(ctx, c.orgID, later.ID, byOperator)
	require.NoError(t, err)
	require.Len(t, restored, 1)
	require.Equal(t, ComponentPSU, restored[0].Component)
	require.Equal(t, psuCapsuleChecksum, restored[0].FirmwareChecksum)
	require.Equal(t, laterCapsuleChecksum, restored[0].PreviousFirmwareChecksum)
	groups := c.groups(t)
	require.Len(t, groups, 2)
	require.Equal(t, psuCapsuleChecksum, groups[ComponentPSU].FirmwareChecksum)
	require.Equal(t, restored[0].ID, groups[ComponentPSU].ActiveRolloutID)
	c.svc.EnforceTick(ctx)
	_, psu = c.sent()
	require.Equal(t, 1, psu)
	sent := c.dispatcher.artifacts[len(c.dispatcher.artifacts)-1]
	require.Equal(t, psuCapsuleChecksum, sent.Checksum)
	require.True(t, sent.Metadata.IsPSU(), "the restored capsule is sent as PSU firmware")
	requireHashboardsUntouched()
}

// Retry acts on one component of a model. Retrying a PSU rollout whose
// container failed sends the capsule again, not the hashboard firmware.
func TestCT1PSURetrySendsTheCapsuleAgain(t *testing.T) {
	c := newCT1Channel(t)
	ctx := t.Context()
	c.hashboardsOnTarget(t)

	started := c.apply(t, ct1PSU)[0]
	// The container never reports the capsule's version.
	for range MaxAttempts + 1 {
		c.svc.EnforceTick(ctx)
		c.observe(t, psuInstalledVersion, false)
		c.advanceClock(time.Hour)
		c.backdateSends(t)
	}
	c.svc.EnforceTick(ctx)
	require.Equal(t, PhaseFailed, phaseOf(c.rollout(t, started.ID), "ct1"))
	hashboards, failedSends := c.sent()
	require.Equal(t, 1, hashboards)
	require.EqualValues(t, MaxAttempts, failedSends)

	retried, err := c.svc.RetryFailedDevices(ctx, c.orgID, started.ID, byOperator)
	require.NoError(t, err)
	require.Equal(t, ComponentPSU, retried.Component)
	require.Equal(t, psuCapsuleChecksum, retried.FirmwareChecksum)
	require.Equal(t, PhaseQueued, phaseOf(*retried, "ct1"))
	c.svc.EnforceTick(ctx)
	hashboards, psu := c.sent()
	require.Equal(t, 1, hashboards, "a PSU retry must not send hashboard firmware")
	require.EqualValues(t, MaxAttempts+1, psu)
	require.True(t, c.dispatcher.artifacts[len(c.dispatcher.artifacts)-1].Metadata.IsPSU())
	groups := c.groups(t)
	require.Equal(t, checksum2, groups[ComponentMain].FirmwareChecksum)
	require.EqualValues(t, 1, groups[ComponentMain].OnTargetCount)
	require.Zero(t, groups[ComponentMain].ActiveRolloutID)
}

// A firmware command keeps a container at UPDATING, so an update that this
// rollout sent cannot verify while a command of another component runs. The
// wait for verification is measured from the end of that command, so the
// container gets a full resend interval to report before it is sent again.
func TestCommandOfAnotherComponentRestartsTheResendInterval(t *testing.T) {
	for _, tc := range []struct {
		name   string
		other  string
		assign Assignment
	}{
		{"a PSU update after hashboard firmware was sent", `{"firmware_file_id":"psu-1","firmware_component":"psu"}`, ct1Hashboards},
		// A command that names no component updates the main firmware.
		{"a hashboard update after PSU firmware was sent", `{"firmware_file_id":"manual"}`, ct1PSU},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCT1Channel(t)
			ctx := t.Context()
			started := c.apply(t, tc.assign)[0]
			c.svc.EnforceTick(ctx)
			require.Len(t, c.dispatcher.artifacts, 1)

			// The other component's command runs for longer than the resend
			// interval. Nothing is sent while it is on the queue.
			_, err := c.conn.ExecContext(ctx, `
				INSERT INTO queue_message (command_batch_log_uuid, device_id, command_type, status, retry_count, payload)
				VALUES ('00000000-0000-0000-0000-000000000000', $1, 'FirmwareUpdate', 'PROCESSING', 0, $2::jsonb)`, c.deviceID, tc.other)
			require.NoError(t, err)
			c.advanceClock(time.Hour)
			c.backdateSends(t)
			c.svc.EnforceTick(ctx)
			require.Len(t, c.dispatcher.artifacts, 1)

			// It ends. The container has not reported yet.
			_, err = c.conn.ExecContext(ctx, `DELETE FROM queue_message WHERE device_id = $1`, c.deviceID)
			require.NoError(t, err)
			_, err = c.conn.ExecContext(ctx, `
				INSERT INTO queue_message (command_batch_log_uuid, device_id, command_type, status, retry_count, payload, updated_at)
				VALUES ('00000000-0000-0000-0000-000000000000', $1, 'FirmwareUpdate', 'SUCCESS', 0, $2::jsonb, $3)`, c.deviceID, tc.other, c.clock)
			require.NoError(t, err)
			c.svc.EnforceTick(ctx)
			require.Len(t, c.dispatcher.artifacts, 1, "the firmware is not sent again as soon as the other command ends")
			waiting := deviceOf(t, c.rollout(t, started.ID), "ct1")
			require.EqualValues(t, 1, waiting.Attempts)
			require.Equal(t, PhaseInProgress, waiting.Phase)

			// A container that still has not reported after a full interval is
			// sent the firmware again.
			c.advanceClock(resendInterval + time.Minute)
			c.svc.EnforceTick(ctx)
			require.Len(t, c.dispatcher.artifacts, 2)
			require.Equal(t, tc.assign.Component, c.dispatcher.artifacts[1].Metadata.ComponentKey())
		})
	}
}

// The same rule holds for a delegated rollout, which fails an unverified
// attempt after the resend interval and does not send again by itself.
func TestDelegatedAttemptWaitsOutACommandOfAnotherComponent(t *testing.T) {
	c := newCT1Channel(t)
	ctx := t.Context()
	_, err := c.svc.UpdateChannel(ctx, c.orgID, c.channelID, ChannelSpec{
		Name: "Test channel", Scope: Scope{DeviceIdentifiers: []string{"ct1"}}, Behavior: delegated(),
	})
	require.NoError(t, err)
	started := c.apply(t, ct1Hashboards)[0]
	_, sent, err := c.svc.AdvanceRollout(ctx, c.orgID, started.ID, DeviceSelection{Count: 1}, byOperator)
	require.NoError(t, err)
	require.Equal(t, []string{"ct1"}, sent)

	// A PSU update runs for longer than the resend interval, then ends.
	const psuCommand = `{"firmware_file_id":"psu-1","firmware_component":"psu"}`
	_, err = c.conn.ExecContext(ctx, `
		INSERT INTO queue_message (command_batch_log_uuid, device_id, command_type, status, retry_count, payload)
		VALUES ('00000000-0000-0000-0000-000000000000', $1, 'FirmwareUpdate', 'PROCESSING', 0, $2::jsonb)`, c.deviceID, psuCommand)
	require.NoError(t, err)
	c.advanceClock(time.Hour)
	c.backdateSends(t)
	c.svc.EnforceTick(ctx)
	require.Equal(t, PhaseInProgress, phaseOf(c.rollout(t, started.ID), "ct1"))
	_, err = c.conn.ExecContext(ctx, `DELETE FROM queue_message WHERE device_id = $1`, c.deviceID)
	require.NoError(t, err)
	_, err = c.conn.ExecContext(ctx, `
		INSERT INTO queue_message (command_batch_log_uuid, device_id, command_type, status, retry_count, payload, updated_at)
		VALUES ('00000000-0000-0000-0000-000000000000', $1, 'FirmwareUpdate', 'SUCCESS', 0, $2::jsonb, $3)`, c.deviceID, psuCommand, c.clock)
	require.NoError(t, err)
	c.svc.EnforceTick(ctx)
	require.Equal(t, PhaseInProgress, phaseOf(c.rollout(t, started.ID), "ct1"), "the attempt is not failed as soon as the PSU update ends")

	// The container reports inside the interval and verifies.
	c.observe(t, psuInstalledVersion, false)
	c.svc.EnforceTick(ctx)
	require.Equal(t, PhaseDone, phaseOf(c.rollout(t, started.ID), "ct1"))
	require.Len(t, c.dispatcher.artifacts, 1)
}
