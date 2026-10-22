package timescaledb

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestLaterContainerSnapshotCanApplyErrorFromOlderHealthEvidence(t *testing.T) {
	r := newContainerRollupRig(t)

	activeAt := r.base.Add(10 * time.Second)
	active := r.snapshot(10*time.Second, false, 100e12, 200e12)
	active.Tanks[0].Modules[0].State = sdk.MinerStatusActive
	active.Tanks[0].Modules[0].ObservedAt = activeAt
	r.store(t, active)

	errorEvidenceAt := r.base.Add(8 * time.Second)
	errorSnapshotAt := r.base.Add(11 * time.Second)
	faulted := r.snapshot(11*time.Second, false, 100e12, 200e12)
	faulted.Tanks[0].Modules[0].State = sdk.MinerStatusError
	faulted.Tanks[0].Modules[0].ObservedAt = errorEvidenceAt
	r.store(t, faulted)

	var moduleID int64
	var state string
	var stateObservedAt time.Time
	require.NoError(t, r.db.QueryRowContext(t.Context(), `
		SELECT m.id, m.state, m.state_observed_at
		FROM container_module m
		JOIN container_tank t ON t.id=m.tank_id
		WHERE t.device_id=$1 AND t.position=1 AND m.position=1 AND m.retired_at IS NULL
	`, r.databaseID).Scan(&moduleID, &state, &stateObservedAt))
	require.Equal(t, sdk.MinerStatusError.String(), state, "the later accepted snapshot determines current status")
	require.Equal(t, errorEvidenceAt, stateObservedAt.UTC(), "status retains the health evidence timestamp")

	var observedState string
	var observedStateAt time.Time
	require.NoError(t, r.db.QueryRowContext(t.Context(), `
		SELECT state, state_observed_at
		FROM container_module_diagnostic_observation
		WHERE module_id=$1 AND collected_at=$2
	`, moduleID, errorSnapshotAt).Scan(&observedState, &observedStateAt))
	require.Equal(t, sdk.MinerStatusError.String(), observedState)
	require.Equal(t, errorEvidenceAt, observedStateAt.UTC())

	// Collection ordering still rejects delayed replays, even when their source
	// evidence is newer than the fault evidence retained by the accepted poll.
	r.store(t, active)
	require.NoError(t, r.db.QueryRowContext(t.Context(), `
		SELECT state, state_observed_at FROM container_module WHERE id=$1
	`, moduleID).Scan(&state, &stateObservedAt))
	require.Equal(t, sdk.MinerStatusError.String(), state)
	require.Equal(t, errorEvidenceAt, stateObservedAt.UTC())
}

func TestSiteMoveFiltersModuleIssueEvidencePerSource(t *testing.T) {
	r := newContainerRollupRig(t)
	ctx := t.Context()
	var orgID, siteA, siteB int64
	require.NoError(t, r.db.QueryRowContext(ctx, "SELECT org_id FROM device WHERE id=$1", r.databaseID).Scan(&orgID))
	require.NoError(t, r.db.QueryRowContext(ctx, "INSERT INTO site(org_id,name,slug) VALUES ($1,'A','a') RETURNING id", orgID).Scan(&siteA))
	require.NoError(t, r.db.QueryRowContext(ctx, "INSERT INTO site(org_id,name,slug) VALUES ($1,'B','b') RETURNING id", orgID).Scan(&siteB))

	move := func(siteID int64) {
		t.Helper()
		_, err := r.db.ExecContext(ctx, "UPDATE device SET site_id=$1 WHERE id=$2", siteID, r.databaseID)
		require.NoError(t, err)
	}

	move(siteA)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))

	move(siteB)
	placementAt := r.base.Add(10 * time.Second)
	mixed := r.snapshot(10*time.Second, false, 100e12, 200e12)
	mixed.Tanks[0].Modules[0].Issues = &sdk.ContainerModuleIssues{
		ObservedAt: r.base,
		Complete:   true,
		SourceTimes: map[string]time.Time{
			"hashboard:old":   r.base,
			"hashboard:fresh": placementAt,
			"hashboard:clear": placementAt,
			"psu":             placementAt,
		},
		Items: []sdk.ContainerModuleIssue{
			{Component: "hashboard", ComponentID: "old", Cause: "OLD_SITE_FAULT", Severity: "fault", ObservedAt: r.base},
			{Component: "hashboard", ComponentID: "fresh", Cause: "NEW_SITE_FAULT", Severity: "fault", ObservedAt: placementAt},
		},
	}
	r.store(t, mixed)

	var moduleID int64
	var currentJSON, sourceTimesJSON []byte
	var issuesObservedAt, attributionSince sql.NullTime
	require.NoError(t, r.db.QueryRowContext(ctx, `
		SELECT m.id, m.issues, m.issues_observed_at, m.issues_source_times, m.attribution_since
		FROM container_module m
		JOIN container_tank t ON t.id=m.tank_id
		WHERE t.device_id=$1 AND t.position=1 AND m.position=1 AND m.retired_at IS NULL
	`, r.databaseID).Scan(&moduleID, &currentJSON, &issuesObservedAt, &sourceTimesJSON, &attributionSince))
	require.True(t, attributionSince.Valid)
	require.Equal(t, placementAt, attributionSince.Time.UTC())
	require.True(t, issuesObservedAt.Valid)
	require.Equal(t, placementAt, issuesObservedAt.Time.UTC())

	assertFilteredModuleIssues(t, currentJSON, placementAt)
	var sourceTimes map[string]time.Time
	require.NoError(t, json.Unmarshal(sourceTimesJSON, &sourceTimes))
	assertIssueSourceTimes(t, sourceTimes, map[string]time.Time{
		"hashboard:old":   r.base,
		"hashboard:fresh": placementAt,
		"hashboard:clear": placementAt,
		"psu":             placementAt,
	})

	var historicalJSON []byte
	require.NoError(t, r.db.QueryRowContext(ctx, `
		SELECT issues
		FROM container_module_diagnostic_observation
		WHERE module_id=$1 AND collected_at=$2
	`, moduleID, placementAt).Scan(&historicalJSON))
	assertFilteredModuleIssues(t, historicalJSON, placementAt)
}

func assertFilteredModuleIssues(t *testing.T, encoded []byte, observedAt time.Time) {
	t.Helper()
	var issues sdk.ContainerModuleIssues
	require.NoError(t, json.Unmarshal(encoded, &issues))
	require.False(t, issues.Complete, "removing pre-placement evidence makes the report incomplete")
	require.True(t, issues.ObservedAt.Equal(observedAt))
	assertIssueSourceTimes(t, issues.SourceTimes, map[string]time.Time{
		"hashboard:fresh": observedAt,
		"hashboard:clear": observedAt,
		"psu":             observedAt,
	})
	require.Len(t, issues.Items, 1)
	require.Equal(t, "hashboard", issues.Items[0].Component)
	require.Equal(t, "fresh", issues.Items[0].ComponentID)
	require.Equal(t, "NEW_SITE_FAULT", issues.Items[0].Cause)
	require.Equal(t, "fault", issues.Items[0].Severity)
	require.True(t, issues.Items[0].ObservedAt.Equal(observedAt))
}

func assertIssueSourceTimes(t *testing.T, actual, expected map[string]time.Time) {
	t.Helper()
	require.Len(t, actual, len(expected))
	for source, expectedAt := range expected {
		actualAt, ok := actual[source]
		require.True(t, ok, source)
		require.True(t, actualAt.Equal(expectedAt), source)
	}
}
