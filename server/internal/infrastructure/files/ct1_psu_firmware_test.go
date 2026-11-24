package files

import (
	"bytes"
	"os"
	"testing"

	"github.com/block/proto-fleet/sdk/mdk/firmware/firmwaretest"
	"github.com/block/proto-fleet/sdk/mdk/psucap"
	"github.com/block/proto-fleet/sdk/mdk/psucap/psucaptest"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/require"
)

func psuCapsule(t *testing.T) ([]byte, *psucap.Capsule) {
	t.Helper()
	data, err := psucaptest.Build(psucaptest.Options{Revision: "HA 12/00"})
	require.NoError(t, err)
	capsule, err := psucap.Verify(data)
	require.NoError(t, err)
	return data, capsule
}

func psuMetadata(version string) FirmwareMetadata {
	return FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "CT1", FirmwareVersion: version, Component: FirmwareComponentPSU}
}

func TestCT1PSUCapsuleIsStoredAsUploaded(t *testing.T) {
	data, capsule := psuCapsule(t)
	t.Chdir(t.TempDir())
	svc, err := NewService(Config{})
	require.NoError(t, err)
	require.Contains(t, AllowedFirmwareExtensions(), ".psucap")
	meta := psuMetadata("app=1410;bl=1110")

	saved, err := svc.SaveFirmwareUpload(t.Context(), "BOCO update.PSUCAP", bytes.NewReader(data), FirmwareMetadata{TargetManufacturer: " Proto ", TargetModel: "CT1", FirmwareVersion: "app=1410;bl=1110", Component: " PSU "}, false)
	require.NoError(t, err)
	require.False(t, saved.Reused)
	require.Equal(t, FirmwareComponentPSU, saved.Metadata.Component)
	require.Equal(t, "psu-firmware-boco-HA_12_00-"+capsule.ID[:12]+".psucap", saved.StoredFilename, "manufacturer text never reaches the file system unescaped")

	reader, info, err := svc.OpenFirmwareFileWithInfo(saved.FirmwareFileID)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	stored, err := os.ReadFile(info.FilePath)
	require.NoError(t, err)
	require.Equal(t, data, stored, "the signed capsule must reach the controller byte for byte")
	require.Equal(t, capsule.ID, info.SHA256, "the stored checksum is the capsule ID Frigate uses")
	require.Equal(t, FirmwareComponentPSU, info.Component)
	require.Empty(t, info.HashboardBuild)

	again, err := svc.SaveFirmwareUpload(t.Context(), "renamed.psucap", bytes.NewReader(data), meta, false)
	require.NoError(t, err)
	require.True(t, again.Reused)
	require.Equal(t, saved.FirmwareFileID, again.FirmwareFileID)
	found, ok := svc.FindFirmwareFileByChecksum(capsule.ID, meta)
	require.True(t, ok)
	require.Equal(t, saved.FirmwareFileID, found)
	_, ok = svc.FindFirmwareFileByChecksum(capsule.ID, FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "CT1", FirmwareVersion: "app=1410;bl=1110", HashboardBuild: "prod"})
	require.False(t, ok, "a capsule never satisfies a hashboard firmware lookup")

	listed, err := svc.ListFirmwareFiles()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, FirmwareComponentPSU, listed[0].Component)
	artifact, err := svc.ResolveFirmwareArtifact(saved.FirmwareFileID)
	require.NoError(t, err)
	require.True(t, artifact.Metadata.IsPSU())

	// The version is the operator's label, so it can be corrected. What the
	// file is cannot change.
	relabeled, err := svc.UpdateFirmwareMetadata(t.Context(), saved.FirmwareFileID, psuMetadata("v1.4"))
	require.NoError(t, err)
	require.Equal(t, "v1.4", relabeled.Current.FirmwareVersion, "a PSU version is not a hashboard version and keeps its text")
	_, err = svc.UpdateFirmwareMetadata(t.Context(), saved.FirmwareFileID, FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "CT1", FirmwareVersion: "1.4.0", HashboardBuild: "prod"})
	require.True(t, fleeterror.IsInvalidArgumentError(err), "got %v", err)
	require.ErrorContains(t, err, "component must match the stored file")
	_, err = svc.UpdateFirmwareMetadata(t.Context(), saved.FirmwareFileID, FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "Rig", FirmwareVersion: "1.4.0"})
	require.ErrorContains(t, err, "component must match the stored file")

	count, err := StagedFirmwareUploadCount()
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestCT1PSUFirmwareUploadRejectsWhatIsNotACapsuleForCT1(t *testing.T) {
	data, _ := psuCapsule(t)
	damaged := bytes.Clone(data)
	damaged[600] ^= 1
	archive, err := os.ReadFile(firmwaretest.Archive(t, map[string][]byte{"prod.bin": firmwaretest.Image(t, "prod", "f1", 2)}))
	require.NoError(t, err)
	t.Chdir(t.TempDir())
	svc, err := NewService(Config{})
	require.NoError(t, err)
	for name, tc := range map[string]struct {
		filename string
		payload  []byte
		meta     FirmwareMetadata
		want     string
	}{
		"capsule as hashboard firmware": {"psu.psucap", data, FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "CT1", FirmwareVersion: "2.0.0", HashboardBuild: "prod"}, "must be uploaded for Proto CT1 with component psu"},
		"capsule renamed to an archive": {"release.zip", data, FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "CT1", FirmwareVersion: "2.0.0", HashboardBuild: "prod"}, "must be uploaded for Proto CT1 with component psu"},
		"capsule for another miner":     {"psu.psucap", data, FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "Rig", FirmwareVersion: "2.0.0"}, "must be uploaded for Proto CT1 with component psu"},
		"capsule name without content":  {"psu.psucap", []byte("firmware"), FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "Rig", FirmwareVersion: "2.0.0"}, "must be uploaded for Proto CT1 with component psu"},
		"archive as PSU firmware":       {"release.zip", archive, psuMetadata("1.4"), "expected a PSUCAP capsule"},
		"damaged capsule":               {"psu.psucap", damaged, psuMetadata("1.4"), "invalid PSU firmware"},
		"oversized capsule":             {"psu.psucap", append(bytes.Clone(data), make([]byte, psucap.MaxSize)...), psuMetadata("1.4"), "expected a PSUCAP capsule of at most"},
		"psu on another target":         {"psu.psucap", data, FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "Rig", FirmwareVersion: "1.4", Component: "psu"}, "component psu is only supported for CT1"},
		"psu with a hashboard build":    {"psu.psucap", data, FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "CT1", FirmwareVersion: "1.4", Component: "psu", HashboardBuild: "prod"}, "hashboard_build does not apply"},
		"unknown component":             {"release.zip", archive, FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "CT1", FirmwareVersion: "2.0.0", Component: "plc"}, "component must be psu when set"},
		"no version":                    {"psu.psucap", data, psuMetadata(""), "firmware_version is required"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.SaveFirmwareUpload(t.Context(), tc.filename, bytes.NewReader(tc.payload), tc.meta, false)
			require.True(t, fleeterror.IsInvalidArgumentError(err), "got %v", err)
			require.ErrorContains(t, err, tc.want)
		})
	}
	listed, err := svc.ListFirmwareFiles()
	require.NoError(t, err)
	require.Empty(t, listed)
	count, err := StagedFirmwareUploadCount()
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestHashboardArchiveCannotBecomePSUFirmware(t *testing.T) {
	archive := firmwaretest.Archive(t, map[string][]byte{"prod.bin": firmwaretest.Image(t, "prod", "f1", 2)})
	t.Chdir(t.TempDir())
	svc, err := NewService(Config{})
	require.NoError(t, err)
	f, err := os.Open(archive)
	require.NoError(t, err)
	saved, err := svc.SaveFirmwareUpload(t.Context(), "release.zip", f, FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "CT1", FirmwareVersion: "2.0.0", HashboardBuild: "prod"}, false)
	require.NoError(t, f.Close())
	require.NoError(t, err)
	_, err = svc.UpdateFirmwareMetadata(t.Context(), saved.FirmwareFileID, psuMetadata("2.0.0"))
	require.True(t, fleeterror.IsInvalidArgumentError(err), "got %v", err)
	require.ErrorContains(t, err, "component must match the stored file")
}
