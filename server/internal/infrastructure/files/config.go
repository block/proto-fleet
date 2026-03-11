package files

// Config holds configuration for the files service.
type Config struct {
	MaxFirmwareFileSize int64 `help:"Maximum firmware file size in bytes." default:"524288000" env:"MAX_FIRMWARE_FILE_SIZE"`
}
