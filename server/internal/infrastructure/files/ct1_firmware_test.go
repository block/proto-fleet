package files

import (
	"os"
	"testing"

	"github.com/block/proto-fleet/sdk/mdk/firmware"
	"github.com/block/proto-fleet/sdk/mdk/firmware/firmwaretest"
	"github.com/stretchr/testify/require"
)

func TestCT1FirmwareBuildsHaveDistinctImmutableArtifacts(t *testing.T) {
	archive := firmwaretest.Archive(t, map[string][]byte{"prod.bin": firmwaretest.Image(t, "prod", "f1", 2), "dev.bin": firmwaretest.Image(t, "dev", "f1", 2), "mfg.bin": firmwaretest.Image(t, "mfgtest", "f1", 2)})
	t.Chdir(t.TempDir())
	svc, err := NewService(Config{})
	require.NoError(t, err)
	checksums := map[string]bool{}
	for _, build := range []string{"prod", "dev", "mfgtest"} {
		meta := FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "CT1", FirmwareVersion: "2.0.0", HashboardBuild: build}
		var first string
		for range 2 {
			f, err := os.Open(archive)
			require.NoError(t, err)
			result, err := svc.SaveFirmwareUpload("release.zip", f, meta, false)
			require.NoError(t, f.Close())
			require.NoError(t, err)
			if first != "" {
				require.Equal(t, first, result.FirmwareFileID)
				require.True(t, result.Reused)
				continue
			}
			first = result.FirmwareFileID
			reader, info, err := svc.OpenFirmwareFileWithInfo(first)
			require.NoError(t, err)
			require.NoError(t, reader.Close())
			require.False(t, checksums[info.SHA256])
			checksums[info.SHA256] = true
			selected, err := firmware.Read(info.FilePath, "")
			require.NoError(t, err)
			require.Equal(t, build, selected.Build)
			require.Len(t, selected.Images, 1)
			wrong := meta
			wrong.FirmwareVersion = "3.0.0"
			_, err = svc.UpdateFirmwareMetadata(first, wrong)
			require.Error(t, err)
		}
	}
	count, err := StagedFirmwareUploadCount()
	require.NoError(t, err)
	require.Zero(t, count)
}
