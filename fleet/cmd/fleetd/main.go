package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/alecthomas/kong"
	"github.com/golang-migrate/migrate/v4"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/btc-mining/miner-firmware/fleet/db"
	"github.com/btc-mining/miner-firmware/fleet/db/sqlc"
	"github.com/btc-mining/miner-firmware/fleet/server"
)

// Config contains all runtime configuration for fleetd.
type Config struct {
	DB db.Config `embed:"" prefix:"db"`

	LogLevel        slog.Level `help:"Log level" default:"debug" env:"LOG_LEVEL"`
	Addr            string     `help:"Address to listen on" default:"127.0.0.1:8080" env:"LISTEN_ADDR"`
	StaticAssetPath string     `help:"Static asset path" env:"STATIC_ASSET_PATH"`
}

func getCurrentPackagePath() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("failed to get package path")
	}
	return filepath.Dir(filename), nil
}

func run(cfg *Config) error {
	conn, err := cfg.DB.Connect()
	if err != nil {
		return err
	}
	slog.Info("Migrating database", slog.String("addr", cfg.DB.Addr), slog.String("db", cfg.DB.Name))
	if err := db.Migrate(conn); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	q := sqlc.New(conn)

	// Just an example of resolving a relative path from the current file. The real static folder should
	// probably be the output of "npm build" somewhere.
	if cfg.StaticAssetPath == "" {
		path, err := getCurrentPackagePath()
		if err != nil {
			return err
		}
		cfg.StaticAssetPath = filepath.Clean(filepath.Join(path, "../../static"))
	}

	slog.Info("serving static assets from", slog.String("path", cfg.StaticAssetPath))
	slog.Info("starting fleet", slog.String("addr", cfg.Addr))
	httpServer := http.Server{
		Addr:              cfg.Addr,
		Handler:           h2c.NewHandler(server.NewMux(cfg.StaticAssetPath, conn, q), &http2.Server{}),
		ReadHeaderTimeout: 3 * time.Second,
	}
	return fmt.Errorf("http server error: %w", httpServer.ListenAndServe())
}

func main() {
	cfg := &Config{}
	_ = kong.Parse(cfg, kong.Name("fleetd"))
	slog.SetLogLoggerLevel(cfg.LogLevel)

	if err := run(cfg); err != nil {
		slog.Error("exit with err", slog.Any("error", err))
		os.Exit(1)
	}
}
