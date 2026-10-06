package updates

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/releaseinfo"
	"github.com/block/proto-fleet/server/internal/updaterapi"
)

func useEmbeddedRepository(t *testing.T, repository string) {
	t.Helper()
	// Build identity is process-global; callers must not run in parallel.
	original := releaseinfo.Repository
	releaseinfo.Repository = repository
	t.Cleanup(func() { releaseinfo.Repository = original })
}

func TestAlternateRepositoryDiscoveryAndCommands(t *testing.T) {
	useEmbeddedRepository(t, "example-owner/fleet-fork")
	t.Setenv("UPDATES_RELEASE_REPOSITORY", "other-owner/fleet")
	t.Setenv("PROTO_FLEET_RELEASE_REPOSITORY", "other-owner/fleet")
	cfg := Config{Enabled: true}
	require.NoError(t, cfg.Validate())
	checker := NewChecker(cfg, "v1.0.0")
	var requests []string
	checker.client.httpClient.Transport = executorRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.String())
		require.Equal(t, "api.github.com", r.URL.Host)
		require.True(t, strings.HasPrefix(r.URL.Path, "/repos/example-owner/fleet-fork/releases"))
		body := `{"tag_name":"v1.1.0"}`
		if strings.HasSuffix(r.URL.Path, "/releases") {
			body = `[{"tag_name":"v1.1.0"},{"tag_name":"v1.2.0-rc.1","prerelease":true},{"tag_name":"nightly-20260917-0123456789ab","prerelease":true}]`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	checker.check(context.Background())
	snapshot := checker.Snapshot()
	require.Len(t, requests, 2)
	require.Equal(t, "v1.1.0", snapshot.LatestStable.Version)
	require.Equal(t, "v1.2.0-rc.1", snapshot.LatestRC.Version)
	require.Equal(t, "https://github.com/example-owner/fleet-fork/releases/tag/v1.1.0", snapshot.LatestStable.NotesURL)
	command, ok := installCommand(releaseinfo.Repository, snapshot.LatestRC.Version)
	require.True(t, ok)
	require.Equal(t, `bash <(curl -fsSL "https://github.com/example-owner/fleet-fork/releases/download/v1.2.0-rc.1/install.sh") v1.2.0-rc.1`, command)

	// A forbidden source invalidates offers; it never triggers an upstream request.
	checker.client.httpClient.Transport = executorRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Contains(t, r.URL.Path, "/repos/example-owner/fleet-fork/")
		return &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("denied"))}, nil
	})
	checker.check(context.Background())
	require.False(t, checker.Snapshot().StableAvailable)
	require.False(t, checker.Snapshot().RCAvailable)
}

func TestConfigRejectsConflictingRepositoryURL(t *testing.T) {
	useEmbeddedRepository(t, "example-owner/fleet-fork")
	cfg := Config{DownloadBaseURL: downloadBaseURL}
	require.ErrorContains(t, cfg.Validate(), "DownloadBaseURL")
	for _, repository := range []string{"owner/../other", "https://github.com/owner/repo", "owner/repo?token=secret", "owner/repo;id"} {
		releaseinfo.Repository = repository
		cfg := Config{}
		require.Error(t, cfg.Validate())
		command, ok := installCommand(repository, "v1.0.0")
		require.False(t, ok)
		require.Empty(t, command)
	}
}

func TestUpdaterSourceMismatchRejectsNewOperation(t *testing.T) {
	useEmbeddedRepository(t, "example-owner/fleet-fork")
	svc := &Service{}
	svc.executor = startExecutorTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/status", r.URL.Path)
		_, _ = io.WriteString(w, `{"release_repository":"block/proto-fleet"}`)
	}))
	require.False(t, svc.executorAvailable(context.Background()))
	_, _, err := svc.currentUpgradeReplay(context.Background(), "operation", "v1.0.0")
	require.ErrorContains(t, err, "repositories do not agree")
}

func TestUpdaterRepositoryChecksExactReplayAndReconciliation(t *testing.T) {
	for _, test := range []struct {
		name       string
		embedded   string
		reported   string
		wantReplay bool
	}{
		{"official", releaseinfo.DefaultRepository, releaseinfo.DefaultRepository, true},
		{"legacy official", releaseinfo.DefaultRepository, "", true},
		{"official rejects fork", releaseinfo.DefaultRepository, "example-owner/fleet-fork", false},
		{"matching fork", "example-owner/fleet-fork", "example-owner/fleet-fork", true},
		{"fork rejects legacy", "example-owner/fleet-fork", "", false},
		{"fork rejects official", "example-owner/fleet-fork", releaseinfo.DefaultRepository, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			useEmbeddedRepository(t, test.embedded)
			for _, phase := range []updaterapi.Phase{updaterapi.PhaseQueued, updaterapi.PhaseSucceeded} {
				t.Run(string(phase), func(t *testing.T) {
					operation := updaterapi.Operation{ID: testOperationID, TargetVersion: "v1.1.0", Phase: phase}
					executor := &fakeExecutor{status: updaterapi.StatusResponse{ReleaseRepository: test.reported, Operation: &operation}}
					svc := &Service{executor: executor}
					ctx := context.Background()
					require.Equal(t, test.wantReplay, svc.executorAvailable(ctx))
					replayed, replay, err := svc.currentUpgradeReplay(ctx, operation.ID, operation.TargetVersion)
					require.Equal(t, test.wantReplay, replay)
					if test.wantReplay {
						require.NoError(t, err)
						require.Equal(t, operation, replayed)
					} else {
						require.ErrorContains(t, err, "repositories do not agree")
						require.Empty(t, replayed)
					}
					reconciled, ok := svc.reconcileUpgrade(ctx, operation.ID, operation.TargetVersion)
					require.Equal(t, test.wantReplay, ok)
					if test.wantReplay {
						require.Equal(t, operation, reconciled)
					} else {
						require.Empty(t, reconciled)
					}
					require.Empty(t, executor.triggered)
				})
			}
		})
	}
}

func TestDiscoveryNeverFollowsRepositoryRedirect(t *testing.T) {
	useEmbeddedRepository(t, "example-owner/fleet-fork")
	checker := NewChecker(Config{Enabled: true}, "v1.0.0")
	requests := 0
	checker.client.httpClient.Transport = executorRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		require.Contains(t, r.URL.Path, "/repos/example-owner/fleet-fork/")
		return &http.Response{
			StatusCode: http.StatusMovedPermanently,
			Header:     http.Header{"Location": []string{"https://api.github.com/repos/block/proto-fleet/releases"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})
	checker.check(context.Background())
	require.Equal(t, 2, requests)
	require.False(t, checker.Snapshot().StableAvailable)
}
