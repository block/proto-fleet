package dto

// FirmwareUpdatePayload is the queue message payload for firmware update commands.
// It references a firmware file previously uploaded to the fleet server.
type FirmwareUpdatePayload struct {
	FirmwareFileID string `json:"firmware_file_id"`
}
