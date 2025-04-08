package logging

import (
	"log/slog"
	"os"
)

type LoggingConfig struct {
	Level slog.Level `help:"Log level" default:"debug" env:"LEVEL"`
	JSON  bool       `help:"Log level" default:"false" env:"JSON"`
}

func InitLogger(config LoggingConfig) {
	logOptions := &slog.HandlerOptions{Level: config.Level}
	var logger *slog.Logger
	if config.JSON {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, logOptions))
	} else {
		logger = slog.New(slog.NewTextHandler(os.Stdout, logOptions))
	}
	slog.SetDefault(logger)
}
