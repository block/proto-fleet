package dto

// FirmwareUpdatePayload is the queue message payload for firmware update commands.
// It references a firmware file previously uploaded to the fleet server.
type FirmwareUpdatePayload struct {
	FirmwareFileID string `json:"firmware_file_id"`
	// FirmwareChecksum binds a release-channel command to its assigned payload.
	// Manual updates omit it and use the file's current metadata at preflight.
	FirmwareChecksum string `json:"firmware_checksum,omitempty"`
	// FirmwareComponent is the firmware component the file updates, as
	// files.FirmwareMetadata.ComponentKey names it: empty for a device's main
	// firmware, "psu" for a CT1 container's PSUs. Release channels keep
	// assignments and deployment provenance per component, so this decides
	// which assignment the command competes with and whose provenance its
	// completion invalidates.
	FirmwareComponent string `json:"firmware_component,omitempty"`
}
