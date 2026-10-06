package files

import "os"

// FindCachedFirmwareFileIDByChecksum reports advisory read availability using
// the checksum inventory verified at upload/startup and current presence and
// readability. It opens, stats and closes candidates without reading payload
// bytes or consulting mutable metadata. It does not attest to current bytes:
// an out-of-band change remains unverified until a strict artifact operation.
// A known verification failure suppresses that exact file snapshot; changes to
// identity, size, modification time or mode allow the candidate to be reconsidered.
// Dispatch, assignment and delivery must continue using their strict verifiers.
func (s *Service) FindCachedFirmwareFileIDByChecksum(checksum string) (string, bool) {
	if validateFirmwareChecksum(checksum) != nil {
		return "", false
	}
	s.firmwareMetadataReuseMu.RLock()
	defer s.firmwareMetadataReuseMu.RUnlock()
	for _, id := range s.firmwareFileIDsByChecksumLocked(checksum) {
		path, err := getFirmwareFilePathForCanonicalID(id)
		if err != nil {
			continue
		}
		// Do not open a known non-regular payload, such as a named pipe.
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		info, statErr := file.Stat()
		closeErr := file.Close()
		if statErr != nil || closeErr != nil || !info.Mode().IsRegular() {
			continue
		}
		s.mu.Lock()
		failed := sameFirmwarePayloadSnapshot(s.firmwarePayloadFailures[id], info)
		s.mu.Unlock()
		if !failed {
			return id, true
		}
	}
	return "", false
}

func sameFirmwarePayloadSnapshot(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() &&
		a.ModTime().Equal(b.ModTime()) && a.Mode() == b.Mode()
}

// Payload failures are separate from metadata-dependent reuse eligibility.
// Only checks against this upload's known identity may suppress availability;
// supplying a wrong checksum to an exact-ID opener cannot poison a healthy file.
func (s *Service) recordFirmwarePayloadVerification(id, expected string, info os.FileInfo, verified bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if info == nil || s.firmwareChecksumByID[id] != expected {
		return
	}
	if verified {
		// Do not let an older successful read clear a concurrent failure for a
		// different file snapshot. Changed snapshots are already reconsidered.
		if sameFirmwarePayloadSnapshot(s.firmwarePayloadFailures[id], info) {
			delete(s.firmwarePayloadFailures, id)
		}
		return
	}
	if s.firmwarePayloadFailures == nil {
		s.firmwarePayloadFailures = make(map[string]os.FileInfo)
	}
	s.firmwarePayloadFailures[id] = info
}
