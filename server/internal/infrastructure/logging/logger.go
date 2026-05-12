package logging

import (
	"log/slog"
	"os"
)

type Config struct {
	Level      slog.Level `help:"Log level" default:"info" env:"LEVEL"`
	JSON       bool       `help:"Log level" default:"false" env:"JSON"`
	BufferSize int        `help:"Log buffer size for export" default:"1000" env:"BUFFER_SIZE"`
}

var defaultBuffer *Buffer

func InitLogger(config Config) {
	logOptions := &slog.HandlerOptions{
		Level:     config.Level,
		AddSource: true,
	}

	var logger slog.Handler
	if config.JSON {
		logger = slog.NewJSONHandler(os.Stdout, logOptions)
	} else {
		logger = slog.NewTextHandler(os.Stdout, logOptions)
	}

	defaultBuffer = NewBuffer(config.BufferSize, config.Level)

	slog.SetDefault(slog.New(newTeeHandler(logger, defaultBuffer)))
}

func DefaultBuffer() *Buffer {
	return defaultBuffer
}
