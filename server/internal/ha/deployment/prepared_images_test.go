package deployment

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreparedImagesAreInspectedWithoutPullOrLoad(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "available", true: "pruned"}[missing], func(t *testing.T) {
			// Arrange
			calls := 0

			// Act
			err := checkApplicationImages(t.Context(), "/release", nil, func(_ context.Context, args []string) ([]byte, error) {
				require.Contains(t, args, "--images")
				require.Contains(t, args, "fleet-api")
				return []byte("proto-fleet-api:v1.1.0\nproto-fleet-client:v1.1.0\n"), nil
			}, func(_ context.Context, name string, args ...string) ([]byte, error) {
				calls++
				require.Equal(t, "docker", name)
				require.Equal(t, []string{"--host", localDockerHost, "image", "inspect", "proto-fleet-api:v1.1.0", "proto-fleet-client:v1.1.0"}, args)
				if missing {
					return nil, os.ErrNotExist
				}
				return []byte("[]"), nil
			})

			// Assert
			require.Equal(t, 1, calls)
			if missing {
				require.ErrorContains(t, err, "no longer available")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
