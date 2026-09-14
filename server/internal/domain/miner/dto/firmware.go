package dto

// FirmwareUpdatePayload is the queue message payload for firmware update commands.
// It references a firmware file previously uploaded to the fleet server.
type FirmwareUpdatePayload struct {
	FirmwareFileID string `json:"firmware_file_id"`
	// FirmwareChecksum binds a release-channel command to its assigned payload.
	// Manual updates omit it and use the file's current metadata at preflight.
	FirmwareChecksum string `json:"firmware_checksum,omitempty"`
}
