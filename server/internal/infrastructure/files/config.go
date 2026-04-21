package files

import "time"

// Config holds configuration for the files service.
type Config struct {
	MaxFirmwareFileSize     int64         `help:"Maximum firmware file size in bytes." default:"524288000" env:"MAX_FIRMWARE_FILE_SIZE"`
	ChunkSizeBytes          int64         `help:"Chunk size for chunked uploads in bytes. Files larger than this use chunked upload." default:"33554432" env:"CHUNK_SIZE_BYTES"`
	ChunkedUploadSessionTTL time.Duration `help:"TTL for abandoned chunked upload sessions." default:"1h" env:"CHUNKED_UPLOAD_SESSION_TTL"`
}
