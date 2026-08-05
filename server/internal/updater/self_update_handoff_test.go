package updater

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelfUpdateHandoffCommitsOnlyAfterReplacementReadiness(t *testing.T) {
	t.Parallel()

	destination := installSelfUpdateForHandoffTest(t)
	startup, err := PrepareSelfUpdateStartup(destination, destination)
	require.NoError(t, err)
	require.NotNil(t, startup)
	assert.FileExists(t, destination+selfUpdateHandoffSuffix)

	require.NoError(t, startup.Commit())
	assert.NoFileExists(t, destination+selfUpdateHandoffSuffix)
	assert.Equal(t, "new updater", mustReadFile(t, destination))
	assert.Equal(t, "old updater", mustReadFile(t, destination+selfUpdateBackupSuffix))

	ordinaryStartup, err := PrepareSelfUpdateStartup(destination, "")
	require.NoError(t, err)
	assert.Nil(t, ordinaryStartup)
}

func TestSelfUpdateHandoffRestoresInterruptedReplacementWithoutArgv(t *testing.T) {
	t.Parallel()

	destination := installSelfUpdateForHandoffTest(t)
	startup, err := PrepareSelfUpdateStartup(destination, "")
	require.ErrorIs(t, err, ErrInterruptedSelfUpdateRestored)
	assert.Nil(t, startup)
	assert.Equal(t, "old updater", mustReadFile(t, destination))
	assert.Equal(t, "old updater", mustReadFile(t, destination+selfUpdateBackupSuffix))
	assert.NoFileExists(t, destination+selfUpdateHandoffSuffix)
}

func TestSelfUpdateHandoffRollbackRestoresReturnedStartupFailure(t *testing.T) {
	t.Parallel()

	destination := installSelfUpdateForHandoffTest(t)
	startup, err := PrepareSelfUpdateStartup(destination, destination)
	require.NoError(t, err)
	require.NoError(t, startup.Rollback())
	assert.Equal(t, "old updater", mustReadFile(t, destination))
	assert.Equal(t, "old updater", mustReadFile(t, destination+selfUpdateBackupSuffix))
	assert.NoFileExists(t, destination+selfUpdateHandoffSuffix)
}

func TestSelfUpdateHandoffReconcilesCrashBeforeCandidateRename(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	destination := filepath.Join(directory, "proto-fleet-updater")
	require.NoError(t, os.WriteFile(destination, []byte("old updater"), 0o755))
	require.NoError(t, os.Link(destination, destination+selfUpdateBackupSuffix))
	require.NoError(t, writeSelfUpdateHandoffMarker(destination))

	startup, err := PrepareSelfUpdateStartup(destination, "")
	require.ErrorIs(t, err, ErrInterruptedSelfUpdateRestored)
	assert.Nil(t, startup)
	assert.Equal(t, "old updater", mustReadFile(t, destination))
	assert.Equal(t, "old updater", mustReadFile(t, destination+selfUpdateBackupSuffix))
	assert.NoFileExists(t, destination+selfUpdateHandoffSuffix)
}

func TestSelfUpdateHandoffReconcilesCrashAfterExecutableRestoration(t *testing.T) {
	t.Parallel()

	destination := installSelfUpdateForHandoffTest(t)
	require.NoError(t, restorePreviousExecutable(destination))
	assert.FileExists(t, destination+selfUpdateHandoffSuffix)

	startup, err := PrepareSelfUpdateStartup(destination, "")
	require.ErrorIs(t, err, ErrInterruptedSelfUpdateRestored)
	assert.Nil(t, startup)
	assert.Equal(t, "old updater", mustReadFile(t, destination))
	assert.Equal(t, "old updater", mustReadFile(t, destination+selfUpdateBackupSuffix))
	assert.NoFileExists(t, destination+selfUpdateHandoffSuffix)
}

func TestSelfUpdateHandoffRestoresMissingExecutableEntry(t *testing.T) {
	t.Parallel()

	destination := installSelfUpdateForHandoffTest(t)
	require.NoError(t, os.Remove(destination))
	assert.NoFileExists(t, destination)

	startup, err := PrepareSelfUpdateStartup(destination, "")
	require.ErrorIs(t, err, ErrInterruptedSelfUpdateRestored)
	assert.Nil(t, startup)
	assert.Equal(t, "old updater", mustReadFile(t, destination))
	assert.Equal(t, "old updater", mustReadFile(t, destination+selfUpdateBackupSuffix))
	assert.NoFileExists(t, destination+selfUpdateHandoffSuffix)
}

func TestSelfUpdateHandoffMismatchFailsClosed(t *testing.T) {
	t.Parallel()

	destination := installSelfUpdateForHandoffTest(t)
	startup, err := PrepareSelfUpdateStartup(destination, filepath.Join(filepath.Dir(destination), "different-updater"))
	require.ErrorContains(t, err, "does not match durable marker")
	assert.Nil(t, startup)
	assert.Equal(t, "new updater", mustReadFile(t, destination))
	assert.Equal(t, "old updater", mustReadFile(t, destination+selfUpdateBackupSuffix))
	assert.FileExists(t, destination+selfUpdateHandoffSuffix)
}

func TestSelfUpdateHandoffRejectsSymlinkMarker(t *testing.T) {
	t.Parallel()

	destination := installSelfUpdateForHandoffTest(t)
	markerPath := destination + selfUpdateHandoffSuffix
	require.NoError(t, os.Remove(markerPath))
	require.NoError(t, os.Symlink(destination+selfUpdateBackupSuffix, markerPath))

	startup, err := PrepareSelfUpdateStartup(destination, destination)
	require.Error(t, err)
	assert.Nil(t, startup)
	assert.Equal(t, "new updater", mustReadFile(t, destination))
}

func installSelfUpdateForHandoffTest(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	destination := filepath.Join(directory, "proto-fleet-updater")
	candidate := filepath.Join(directory, "proto-fleet-updater.candidate")
	require.NoError(t, os.WriteFile(destination, []byte("old updater"), 0o755))
	require.NoError(t, os.WriteFile(candidate, []byte("new updater"), 0o755))
	require.NoError(t, installExecutableCandidate(candidate, destination))
	return destination
}
