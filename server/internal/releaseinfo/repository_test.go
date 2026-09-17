package releaseinfo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateRepository(t *testing.T) {
	t.Parallel()
	for _, repository := range []string{"block/proto-fleet", "example-owner/fleet_fork.test"} {
		require.NoError(t, ValidateRepository(repository))
	}
	for _, repository := range []string{
		"", "owner", "/repo", "owner/", "owner/repo/extra", " owner/repo",
		"owner/repo?x=1", "owner/repo#fragment", "owner/../repo", "owner/repo$(id)",
		"https://github.com/owner/repo", "owner\\repo",
	} {
		require.Error(t, ValidateRepository(repository), repository)
	}
}

func TestOfficialAndAlternateURLs(t *testing.T) {
	t.Parallel()
	require.Equal(t, "https://api.github.com/repos/block/proto-fleet", APIBaseURL(DefaultRepository))
	require.Equal(t, "https://github.com/acme/fleet/releases/download", DownloadBaseURL("acme/fleet"))
	require.Equal(t, "https://github.com/acme/fleet/releases/tag/", ReleaseNotesBaseURL("acme/fleet"))
	require.Equal(t, "https://raw.githubusercontent.com/acme/fleet/nightly-channel/latest.txt", NightlyPointerURL("acme/fleet"))
}

func TestRepositoryFromMetadata(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ metadata, repository string }{
		{"version: v1.0.0\n", DefaultRepository},
		{"version: v1.0.0\nrelease_repository: example-owner/fleet-fork\n", "example-owner/fleet-fork"},
	} {
		repository, err := RepositoryFromMetadata([]byte(test.metadata))
		require.NoError(t, err)
		require.Equal(t, test.repository, repository)
	}
	for _, metadata := range []string{
		"release_repository=example/fleet", " release_repository: example/fleet",
		"release_repository: ", "release_repository: example/fleet\r\n",
		"release_repository: example/fleet\nrelease_repository: example/fleet",
	} {
		_, err := RepositoryFromMetadata([]byte(metadata))
		require.Error(t, err, metadata)
	}
}
