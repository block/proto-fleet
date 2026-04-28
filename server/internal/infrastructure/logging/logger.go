package logging

import (
	"log/slog"
	"os"
)

type Config struct {
	Level      slog.Level `help:"Log level" default:"debug" env:"LEVEL"`
	JSON       bool       `help:"Log level" default:"false" env:"JSON"`
	BufferSize uint       `help:"Log buffer size for export" default:"1000" env:"BUFFER_SIZE"`
}

func InitLogger(config Config) *Buffer {
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

	buf := NewBuffer(int(config.BufferSize), config.Level)

	slog.SetDefault(slog.New(newTeeHandler(logger, buf)))

	return buf
}
