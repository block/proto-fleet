package files

import (
	"errors"
	"sort"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// FirmwareArtifact is a firmware file resolved for a release channel
// assignment: its payload checksum, the identity under which rollouts track
// it, and the metadata snapshotted onto the assignment.
type FirmwareArtifact struct {
	FileID   string
	Checksum string
	Metadata FirmwareMetadata
}

// ResolveFirmwareArtifact returns the checksum and metadata of an uploaded
// firmware file for a release channel assignment. The metadata must satisfy
// the full upload rules (ValidateFirmwareUploadMetadata), since the
// assignment snapshots it and enforces the version; a legacy sidecar without
// firmware_version must be repaired first. A missing file is NotFound and a
// missing or incomplete sidecar InvalidArgument.
func (s *Service) ResolveFirmwareArtifact(fileID string) (FirmwareArtifact, error) {
	canonical, err := canonicalizeFirmwareFileID(fileID)
	if err != nil {
		return FirmwareArtifact{}, err
	}
	filePath, err := getFirmwareFilePathForCanonicalID(canonical)
	if err != nil {
		return FirmwareArtifact{}, err
	}
	metadata, err := readFirmwareMetadata(getFirmwareDirPath(canonical))
	if err != nil {
		if errors.Is(err, errFirmwareMetadataNotFound) {
			return FirmwareArtifact{}, fleeterror.NewInvalidArgumentErrorf("firmware file %s has no metadata", fileID)
		}
		return FirmwareArtifact{}, fleeterror.NewInternalErrorf("failed to read firmware metadata for %s: %v", fileID, err)
	}
	if err := ValidateFirmwareUploadMetadata(metadata); err != nil {
		return FirmwareArtifact{}, fleeterror.NewInvalidArgumentErrorf("firmware file %s metadata is incomplete: %v", fileID, err)
	}
	checksum, err := s.firmwareChecksum(canonical, filePath, true)
	if err != nil {
		return FirmwareArtifact{}, err
	}
	return FirmwareArtifact{FileID: canonical, Checksum: checksum, Metadata: metadata.normalized()}, nil
}

// FirmwareFileIDsByChecksum returns every uploaded firmware file whose payload
// has the given SHA-256, whatever its name or metadata: under the release
// channel artifact identity rule any such file carries the assignment. It
// reads the by-id checksum map, which covers every payload on disk, not the
// reuse index, which covers only files with valid metadata.
func (s *Service) FirmwareFileIDsByChecksum(sha256Hex string) []string {
	s.firmwareMetadataReuseMu.RLock()
	defer s.firmwareMetadataReuseMu.RUnlock()

	s.mu.Lock()
	var ids []string
	for id, checksum := range s.firmwareChecksumByID {
		if checksum == sha256Hex {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	sort.Strings(ids)

	present := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, err := getFirmwareFilePathForCanonicalID(id); err == nil {
			present = append(present, id)
		}
	}
	return present
}

// FindFirmwareFileIDByChecksum returns one uploaded firmware file carrying the
// checksum, or false while none is uploaded; the assignment is then
// unavailable until a payload with that checksum is uploaded again.
func (s *Service) FindFirmwareFileIDByChecksum(sha256Hex string) (string, bool) {
	ids := s.FirmwareFileIDsByChecksum(sha256Hex)
	if len(ids) == 0 {
		return "", false
	}
	return ids[0], true
}
