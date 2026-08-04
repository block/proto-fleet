package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/block/proto-fleet/server/internal/updater"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	defaultInstallRoot := os.Getenv("PROTO_FLEET_INSTALL_ROOT")
	if defaultInstallRoot == "" {
		defaultInstallRoot = "/opt/proto-fleet"
	}
	defaultStateDir := os.Getenv("PROTO_FLEET_UPDATER_STATE_DIR")
	if defaultStateDir == "" {
		defaultStateDir = "/var/lib/proto-fleet-updater"
	}
	defaultSocketPath := os.Getenv("PROTO_FLEET_UPDATER_SOCKET_PATH")
	if defaultSocketPath == "" {
		defaultSocketPath = "/run/proto-fleet-updater/updater.sock"
	}
	defaultSelfUpdatePath := os.Getenv("PROTO_FLEET_UPDATER_BINARY_PATH")

	installRoot := flag.String("install-root", defaultInstallRoot, "Proto Fleet installation root")
	stateDir := flag.String("state-dir", defaultStateDir, "Durable updater state directory")
	socketPath := flag.String("socket-path", defaultSocketPath, "Unix socket path")
	selfUpdatePath := flag.String("self-update-path", defaultSelfUpdatePath, "Installed updater binary path to atomically refresh")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return nil
	}
	absoluteInstallRoot, err := filepath.Abs(*installRoot)
	if err != nil {
		return fmt.Errorf("resolve install root: %w", err)
	}
	manager, err := updater.NewManager(updater.Config{
		InstallRoot:    absoluteInstallRoot,
		StateDir:       *stateDir,
		SelfUpdatePath: *selfUpdatePath,
	})
	if err != nil {
		return fmt.Errorf("initialize updater: %w", err)
	}
	defer func() {
		if err := manager.Close(); err != nil {
			log.Printf("close updater manager: %v", err)
		}
	}()
	server := updater.NewServer(manager)
	errs := make(chan error, 1)
	go func() {
		log.Printf("proto-fleet-updater %s listening on %s", version, *socketPath)
		errs <- server.Serve(*socketPath)
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case sig := <-signals:
		log.Printf("received %s, shutting down", sig)
		if err := shutdownUpdater(server, manager); err != nil {
			log.Printf("shutdown: %v", err)
		}
		return nil
	case <-manager.SelfUpdateReady():
		log.Printf("activating refreshed host updater")
		if err := shutdownUpdater(server, manager); err != nil {
			return fmt.Errorf("drain updater before self-restart: %w", err)
		}
		if *selfUpdatePath == "" {
			return fmt.Errorf("self-update completed without a configured executable path")
		}
		if err := syscall.Exec(*selfUpdatePath, os.Args, os.Environ()); err != nil {
			return fmt.Errorf("activate refreshed updater: %w", err)
		}
		return nil
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve updater API: %w", err)
		}
		return nil
	}
}

func shutdownUpdater(server *updater.Server, manager *updater.Manager) error {
	serverShutdownCtx, cancelServerShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	serverErr := server.Shutdown(serverShutdownCtx)
	cancelServerShutdown()
	managerErr := manager.Shutdown(context.Background())
	return errors.Join(serverErr, managerErr)
}
