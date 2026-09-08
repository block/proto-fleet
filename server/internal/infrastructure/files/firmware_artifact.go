package files

import (
	"errors"
	"sort"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// CanonicalFirmwareFileID returns the lowercase hyphenated form of a firmware
// file id, the form files are stored and reported under, or InvalidArgument
// when the id is not a UUID. Callers that persist a file id (queued
// FirmwareUpdate commands, assignments) store this form so ids compare equal.
func CanonicalFirmwareFileID(fileID string) (string, error) {
	return canonicalizeFirmwareFileID(fileID)
}

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
// channel artifact identity rule a queued update for any of them is an update
// to the assigned artifact. It reads the by-id checksum map, which covers
// every payload on disk, not the reuse index, which covers only files with
// valid metadata.
func (s *Service) FirmwareFileIDsByChecksum(sha256Hex string) []string {
	s.firmwareMetadataReuseMu.RLock()
	defer s.firmwareMetadataReuseMu.RUnlock()
	return s.firmwareFileIDsByChecksumLocked(sha256Hex)
}

// FindDispatchableFirmwareFileID returns a file carrying the checksum that a
// FirmwareUpdate for miners of the (manufacturer, model) pair will accept.
// Command preflight leases the file's current sidecar and requires a known
// target that matches every device, so a payload without a sidecar, with an
// unreadable one, or whose target was edited to another pair is not offered;
// the assignment is then unavailable until a suitable file is uploaded or
// the sidecar repaired.
func (s *Service) FindDispatchableFirmwareFileID(sha256Hex, manufacturer, model string) (string, bool) {
	s.firmwareMetadataReuseMu.RLock()
	defer s.firmwareMetadataReuseMu.RUnlock()

	for _, id := range s.firmwareFileIDsByChecksumLocked(sha256Hex) {
		metadata, err := readFirmwareMetadata(getFirmwareDirPath(id))
		if err != nil || ValidateFirmwareMetadata(metadata) != nil || !metadata.MatchesTarget(manufacturer, model) {
			continue
		}
		return id, true
	}
	return "", false
}

// firmwareFileIDsByChecksumLocked lists the present payloads with the checksum
// in id order. The caller holds firmwareMetadataReuseMu.
func (s *Service) firmwareFileIDsByChecksumLocked(sha256Hex string) []string {
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
