// Package releaseinfo defines the GitHub repository that publishes a Proto Fleet release.
package releaseinfo

import (
	"fmt"
	"regexp"
	"strings"
)

const DefaultRepository = "block/proto-fleet"

// Repository is replaced by release builds with -ldflags. Source builds use
// the official repository. Runtime configuration cannot override this identity.
var Repository = DefaultRepository

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?/[A-Za-z0-9](?:[A-Za-z0-9._-]{0,98}[A-Za-z0-9._-])?$`)

// ValidateRepository accepts exactly one GitHub owner/repository pair. It
// intentionally rejects URL syntax and all characters that are meaningful to
// shells or URL path traversal.
func ValidateRepository(repository string) error {
	if !repositoryPattern.MatchString(repository) || strings.Contains(repository, "..") {
		return fmt.Errorf("release repository must be a GitHub owner/repo value")
	}
	return nil
}

// RepositoryFromMetadata reads the release_repository entry in version.txt.
// Missing identity is the official legacy format; malformed or duplicate
// entries are errors, never reasons to choose another repository.
func RepositoryFromMetadata(contents []byte) (string, error) {
	repository := DefaultRepository
	found := false
	for _, line := range strings.Split(string(contents), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "release_repository") {
			continue
		}
		value, ok := strings.CutPrefix(line, "release_repository: ")
		if !ok || found {
			return "", fmt.Errorf("invalid or duplicate release_repository metadata")
		}
		found = true
		repository = value
		if err := ValidateRepository(repository); err != nil {
			return "", err
		}
	}
	return repository, nil
}

func APIBaseURL(repository string) string {
	return "https://api.github.com/repos/" + repository
}

func ReleasesURL(repository string) string {
	return "https://github.com/" + repository + "/releases"
}

func DownloadBaseURL(repository string) string {
	return ReleasesURL(repository) + "/download"
}

func ReleaseNotesBaseURL(repository string) string {
	return ReleasesURL(repository) + "/tag/"
}

func NightlyPointerURL(repository string) string {
	return "https://raw.githubusercontent.com/" + repository + "/nightly-channel/latest.txt"
}
