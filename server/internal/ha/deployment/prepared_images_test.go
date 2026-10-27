package deployment

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func testPreparedComposeImages(context.Context, []string) ([]byte, error) {
	return []byte("proto-fleet-api:v1.1.0\nproto-fleet-client:v1.1.0\n"), nil
}

func TestPreparedImagesAreBoundToLoadedIdentitiesWithoutPullOrLoad(t *testing.T) {
	for _, mutation := range []string{"unchanged", "retagged API", "retagged sidecar", "pruned", "incomplete inspect", "missing proof", "malformed proof", "empty proof", "changed service set"} {
		t.Run(mutation, func(t *testing.T) {
			// Arrange: record all application and enabled sidecar images from Docker.
			root := t.TempDir()
			profile := fleetApplicationProfile{"ENABLE_BETA_ALERTS": "true", "ENABLE_TRACING": "true"}
			images := []string{"proto-fleet-api:v1.1.0", "proto-fleet-client:v1.1.0", "grafana:12", "otel:1"}
			ids := []string{"sha256:api", "sha256:client", "sha256:grafana", "sha256:otel"}
			composeOutput := func(_ context.Context, args []string) ([]byte, error) {
				require.Contains(t, args, "--images")
				require.Contains(t, args, "fleet-api")
				require.Contains(t, args, "fleet-client")
				require.Contains(t, args, "grafana")
				require.Contains(t, args, "otel-collector")
				return []byte(strings.Join(images, "\n")), nil
			}
			missing := false
			run := func(_ context.Context, name string, args ...string) ([]byte, error) {
				require.Equal(t, "docker", name)
				require.Equal(t, append([]string{"--host", localDockerHost, "image", "inspect", "--format", "{{.Id}}"}, images...), args)
				if missing {
					return nil, os.ErrNotExist
				}
				return []byte(strings.Join(ids, "\n")), nil
			}
			require.NoError(t, recordPreparedApplicationImages(t.Context(), root, profile, composeOutput, run))
			proofPath := filepath.Join(root, preparedImagesFilename)
			proof, err := os.ReadFile(proofPath)
			require.NoError(t, err)
			require.JSONEq(t, `{"proto-fleet-api:v1.1.0":"sha256:api","proto-fleet-client:v1.1.0":"sha256:client","grafana:12":"sha256:grafana","otel:1":"sha256:otel"}`, string(proof))
			switch mutation {
			case "unchanged":
				// Docker/Compose order need not remain the same across invocations.
				slices.Reverse(images)
				slices.Reverse(ids)
			case "retagged API":
				ids[0] = "sha256:replacement"
			case "retagged sidecar":
				ids[2] = "sha256:replacement"
			case "pruned":
				missing = true
			case "incomplete inspect":
				ids = ids[:1]
			case "missing proof":
				require.NoError(t, os.Remove(proofPath))
			case "malformed proof":
				require.NoError(t, os.WriteFile(proofPath, []byte("invalid"), 0o600))
			case "empty proof":
				require.NoError(t, os.WriteFile(proofPath, []byte("{}"), 0o600))
			case "changed service set":
				images = images[:2]
				ids = ids[:2]
			}

			// Act: activation only inspects the current tags; it never pulls/loads.
			err = checkApplicationImages(t.Context(), root, profile, composeOutput, run)

			// Assert
			if mutation == "unchanged" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "prepare the release again")
			}
		})
	}
}
