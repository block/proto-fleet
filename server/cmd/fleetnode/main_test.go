package main

import (
	"bytes"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/releaseinfo"
)

func TestBuildIdentityFlagsPrintAndExit(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"--version":                  version + "\n",
		"--print-release-repository": releaseinfo.Repository + "\n",
	}
	for flag, want := range tests {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()

			// Arrange
			var stdout bytes.Buffer
			exitCode := -1
			parser, err := kong.New(&CLI{},
				kong.Name("fleetnode"),
				kong.Vars{"version": version},
				kong.Writers(&stdout, &bytes.Buffer{}),
				kong.Exit(func(code int) { exitCode = code }),
			)
			require.NoError(t, err)

			// Act
			_, _ = parser.Parse([]string{flag})

			// Assert
			assert.Equal(t, 0, exitCode)
			assert.Equal(t, want, stdout.String())
		})
	}
}
