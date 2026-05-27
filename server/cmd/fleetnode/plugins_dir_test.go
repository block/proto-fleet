//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePluginsDir_Default_BinaryAdjacent(t *testing.T) {
	// Arrange
	exeDir := t.TempDir()
	candidate := filepath.Join(exeDir, "plugins")
	require.NoError(t, os.Mkdir(candidate, 0o755)) //nolint:gosec // test fixture

	// Act
	got, err := resolvePluginsDir(exeDir)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, candidate, got)
}

func TestResolvePluginsDir_Default_MissingReturnsEmpty(t *testing.T) {
	// Arrange
	exeDir := t.TempDir()

	// Act
	got, err := resolvePluginsDir(exeDir)

	// Assert
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestResolvePluginsDir_Default_PresentButUnsafeIsHardError(t *testing.T) {
	// Arrange
	exeDir := t.TempDir()
	candidate := filepath.Join(exeDir, "plugins")
	require.NoError(t, os.Mkdir(candidate, 0o755)) //nolint:gosec // test fixture
	require.NoError(t, os.Chmod(candidate, 0o777)) //nolint:gosec // test exercises the reject path

	// Act
	_, err := resolvePluginsDir(exeDir)

	// Assert
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "writable")
}

func TestResolvePluginsDir_NoExeDir(t *testing.T) {
	// Act
	got, err := resolvePluginsDir("")

	// Assert
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestResolvePluginsDir_Default_FileAtCandidateIgnored(t *testing.T) {
	// Arrange
	exeDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(exeDir, "plugins"), []byte("not a dir"), 0o644))

	// Act
	got, err := resolvePluginsDir(exeDir)

	// Assert
	require.NoError(t, err)
	assert.Empty(t, got)
}

func newPluginsDir(t *testing.T) (exeDir, plugins string) {
	t.Helper()
	exeDir = t.TempDir()
	plugins = filepath.Join(exeDir, "plugins")
	require.NoError(t, os.Mkdir(plugins, 0o755)) //nolint:gosec // test fixture
	return exeDir, plugins
}

func TestValidatePluginFiles_AcceptsOwnedAndTightExecutable(t *testing.T) {
	// Arrange
	_, plugins := newPluginsDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(plugins, "x"), []byte("#!/bin/sh\n"), 0o755))

	// Act
	err := validatePluginFiles(plugins)

	// Assert
	require.NoError(t, err)
}

func TestValidatePluginFiles_RejectsSymlink(t *testing.T) {
	// Arrange
	_, plugins := newPluginsDir(t)
	target := filepath.Join(t.TempDir(), "elsewhere")
	require.NoError(t, os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.Symlink(target, filepath.Join(plugins, "x")))

	// Act
	err := validatePluginFiles(plugins)

	// Assert
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "symlink")
}

func TestValidatePluginFiles_RejectsWorldWritableExecutable(t *testing.T) {
	// Arrange
	_, plugins := newPluginsDir(t)
	path := filepath.Join(plugins, "x")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.Chmod(path, 0o777)) //nolint:gosec // test exercises the reject path

	// Act
	err := validatePluginFiles(plugins)

	// Assert
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "writable")
}

func TestValidatePluginFiles_AcceptsOwnedNonExecutableFiles(t *testing.T) {
	// Arrange — a non-executable file owned by the running uid and not
	// group/world-writable is fine. The check now runs for every regular
	// file (not just executables) but a benign README still passes.
	_, plugins := newPluginsDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(plugins, "README.md"), []byte("docs"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(plugins, "x"), []byte("#!/bin/sh\n"), 0o755))

	// Act
	err := validatePluginFiles(plugins)

	// Assert
	require.NoError(t, err)
}

func TestValidatePluginFiles_RejectsWritableNonExecutableFile(t *testing.T) {
	// Arrange — a non-executable file that is group/world-writable can be
	// chmod +x'd by another local user between validation and plugin load,
	// becoming a stealth RCE vector. Reject before reaching that window.
	_, plugins := newPluginsDir(t)
	path := filepath.Join(plugins, "config.txt")
	require.NoError(t, os.WriteFile(path, []byte("data"), 0o644))
	require.NoError(t, os.Chmod(path, 0o646)) //nolint:gosec // test exercises the reject path

	// Act
	err := validatePluginFiles(plugins)

	// Assert
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "writable")
}

func TestValidatePluginFiles_IgnoresSubdirectories(t *testing.T) {
	// Arrange
	_, plugins := newPluginsDir(t)
	require.NoError(t, os.Mkdir(filepath.Join(plugins, "data"), 0o755)) //nolint:gosec // test fixture
	require.NoError(t, os.WriteFile(filepath.Join(plugins, "x"), []byte("#!/bin/sh\n"), 0o755))

	// Act
	err := validatePluginFiles(plugins)

	// Assert
	require.NoError(t, err)
}

func TestResolvePluginsDir_Default_BadFileBlocksResolution(t *testing.T) {
	// Arrange
	exeDir, plugins := newPluginsDir(t)
	bad := filepath.Join(plugins, "evil")
	require.NoError(t, os.WriteFile(bad, []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.Chmod(bad, 0o777)) //nolint:gosec // test exercises the reject path

	// Act
	_, err := resolvePluginsDir(exeDir)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "evil")
}

func TestResolvePluginsDir_RejectsSymlinkedDir(t *testing.T) {
	// Arrange — <exe-dir>/plugins is a symlink to a real directory. The
	// orphan reaper resolves symlinks via EvalSymlinks; the plugin loader
	// would exec the unresolved path. Refuse to follow at resolve time so
	// the two stay aligned.
	exeDir := t.TempDir()
	target := t.TempDir()
	require.NoError(t, os.Symlink(target, filepath.Join(exeDir, "plugins")))

	// Act
	_, err := resolvePluginsDir(exeDir)

	// Assert
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "symlink")
}

func TestCheckPluginsDirPerms_RejectsEachWritableBitIndependently(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		mode os.FileMode
	}{
		{name: "group writable only", mode: 0o775},
		{name: "world writable only", mode: 0o757},
		{name: "both group and world", mode: 0o777},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			exeDir := t.TempDir()
			candidate := filepath.Join(exeDir, "plugins")
			require.NoError(t, os.Mkdir(candidate, 0o755)) //nolint:gosec // test fixture
			require.NoError(t, os.Chmod(candidate, tc.mode))

			// Act
			_, err := resolvePluginsDir(exeDir)

			// Assert
			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), "writable")
		})
	}
}

func TestValidatePathChain_RejectsWritableAncestor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		mode os.FileMode
	}{
		{name: "group writable parent", mode: 0o775},
		{name: "world writable parent", mode: 0o757},
		{name: "fully writable parent", mode: 0o777},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Arrange — an attacker-writable ancestor lets another user
			// swap our validated plugins dir between the check and the
			// loader's exec. validatePathChain must reject the ancestor
			// regardless of whether the leaf itself is fine.
			base := t.TempDir()
			parent := filepath.Join(base, "writable")
			require.NoError(t, os.Mkdir(parent, 0o755)) //nolint:gosec // test fixture
			candidate := filepath.Join(parent, "plugins")
			require.NoError(t, os.Mkdir(candidate, 0o755)) //nolint:gosec // test fixture
			require.NoError(t, os.Chmod(parent, tc.mode))

			// Act
			err := validatePathChain(candidate)

			// Assert
			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), "writable")
		})
	}
}

func TestValidatePathChain_AllowsStickyWritableAncestor(t *testing.T) {
	t.Parallel()

	// Arrange — /tmp-style sticky+writable ancestors (mode 0o1777) are
	// acceptable because the sticky bit prevents other users from
	// deleting or renaming our dir even though they can create siblings.
	base := t.TempDir()
	sticky := filepath.Join(base, "sticky")
	require.NoError(t, os.Mkdir(sticky, 0o755)) //nolint:gosec // test fixture
	candidate := filepath.Join(sticky, "plugins")
	require.NoError(t, os.Mkdir(candidate, 0o755))             //nolint:gosec // test fixture
	require.NoError(t, os.Chmod(sticky, 0o1777|os.ModeSticky)) //nolint:gosec // test fixture

	// Act
	err := validatePathChain(candidate)

	// Assert
	require.NoError(t, err)
}

func TestValidatePathChain_RequiresAbsolutePath(t *testing.T) {
	// Act
	err := validatePathChain("relative/path")

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absolute")
}
