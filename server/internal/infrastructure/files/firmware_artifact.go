package files

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sort"
	"strings"

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
// has the given SHA-256, whatever its name or metadata. Legacy queued updates
// without a checksum use these IDs to match the assigned artifact; newer
// commands carry their own authoritative checksum. It reads the by-id map, which covers
// every payload on disk, not the reuse index, which covers only files with
// valid metadata.
func (s *Service) FirmwareFileIDsByChecksum(sha256Hex string) []string {
	s.firmwareMetadataReuseMu.RLock()
	defer s.firmwareMetadataReuseMu.RUnlock()
	return s.firmwareFileIDsByChecksumLocked(sha256Hex)
}

// FindFirmwareFileIDByChecksum verifies that a readable payload carries the
// assigned checksum, independent of its current name or metadata. Dispatch must use the
// assignment's metadata snapshot to validate targets and LeaseFirmwareArtifact
// to keep the selected payload present until its command is queued.
func (s *Service) FindFirmwareFileIDByChecksum(sha256Hex string) (string, bool) {
	reader, info, err := s.OpenFirmwareArtifactByChecksum(sha256Hex)
	if err != nil {
		return "", false
	}
	if err := reader.Close(); err != nil {
		return "", false
	}
	return info.ID, true
}

// LeaseFirmwareArtifact verifies cached candidates against the assignment's
// checksum and leases the first healthy payload, skipping corrupt or unreadable
// copies. It holds the lifecycle read lock through command preflight and enqueue.
// The caller validates miners using the saved assignment metadata, then invokes
// release. No mutable sidecar is consulted. Delivery verifies the bytes again.
func (s *Service) LeaseFirmwareArtifact(sha256Hex string) (fileID string, release func(), err error) {
	if err := validateFirmwareChecksum(sha256Hex); err != nil {
		return "", nil, err
	}
	s.firmwareMetadataReuseMu.RLock()
	// Use the private helper: taking another lifecycle RLock through the
	// public opener can deadlock if a writer is waiting.
	reader, info, err := s.openFirmwareArtifactByChecksumLocked(sha256Hex)
	if err != nil {
		s.firmwareMetadataReuseMu.RUnlock()
		return "", nil, err
	}
	if err := reader.Close(); err != nil {
		s.firmwareMetadataReuseMu.RUnlock()
		return "", nil, fleeterror.NewInternalErrorf("failed to close firmware artifact: %v", err)
	}
	return info.ID, s.firmwareMetadataReuseMu.RUnlock, nil
}

// OpenFirmwareArtifactByChecksum resolves the first currently healthy copy of
// an assignment's artifact, independent of its enqueue-time file ID. The returned
// file info identifies that actual copy, and the verified reader starts at byte
// zero. Callers enforcing a grant for a specific file must use OpenFirmwareArtifact.
func (s *Service) OpenFirmwareArtifactByChecksum(sha256Hex string) (io.ReadCloser, FirmwareFileInfo, error) {
	if err := validateFirmwareChecksum(sha256Hex); err != nil {
		return nil, FirmwareFileInfo{}, err
	}
	s.firmwareMetadataReuseMu.RLock()
	defer s.firmwareMetadataReuseMu.RUnlock()
	return s.openFirmwareArtifactByChecksumLocked(sha256Hex)
}

// openFirmwareArtifactByChecksumLocked shares byte verification between
// availability, admission and execution. The caller holds the lifecycle lock.
func (s *Service) openFirmwareArtifactByChecksumLocked(sha256Hex string) (io.ReadCloser, FirmwareFileInfo, error) {
	ids := s.firmwareFileIDsByChecksumLocked(sha256Hex)
	var candidateErr error
	for _, id := range ids {
		reader, info, openErr := s.openFirmwareFileWithInfo(id, sha256Hex)
		if openErr != nil {
			candidateErr = openErr
			continue
		}
		return reader, info, nil
	}
	if candidateErr != nil {
		return nil, FirmwareFileInfo{}, candidateErr
	}
	return nil, FirmwareFileInfo{}, fleeterror.NewNotFoundErrorf("firmware artifact not found: %s", sha256Hex)
}

// OpenFirmwareArtifact opens the exact payload named by a download grant,
// without consulting its mutable sidecar, and verifies the expected checksum
// against the opened bytes. It never substitutes another file ID. The caller
// closes the reader, positioned at the start of the verified payload.
func (s *Service) OpenFirmwareArtifact(fileID, sha256Hex string) (io.ReadCloser, FirmwareFileInfo, error) {
	if err := validateFirmwareChecksum(sha256Hex); err != nil {
		return nil, FirmwareFileInfo{}, err
	}
	s.firmwareMetadataReuseMu.RLock()
	defer s.firmwareMetadataReuseMu.RUnlock()
	return s.openFirmwareFileWithInfo(fileID, sha256Hex)
}

// firmwareArtifactChecksum hashes the same descriptor that will be delivered,
// then rewinds it. Cached upload checksums cannot verify the current bytes.
func firmwareArtifactChecksum(file io.ReadSeeker) (string, error) {
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to compute firmware artifact checksum: %v", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to rewind firmware artifact: %v", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func validateFirmwareChecksum(checksum string) error {
	if len(checksum) != sha256.Size*2 || strings.ToLower(checksum) != checksum {
		return fleeterror.NewInvalidArgumentError("firmware checksum must be a lowercase hex SHA-256")
	}
	if _, err := hex.DecodeString(checksum); err != nil {
		return fleeterror.NewInvalidArgumentError("firmware checksum must be a lowercase hex SHA-256")
	}
	return nil
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
