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
		return
	}
	absoluteInstallRoot, err := filepath.Abs(*installRoot)
	if err != nil {
		log.Fatal(err)
	}
	manager, err := updater.NewManager(updater.Config{
		InstallRoot:    absoluteInstallRoot,
		StateDir:       *stateDir,
		SelfUpdatePath: *selfUpdatePath,
	})
	if err != nil {
		log.Fatalf("initialize updater: %v", err)
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
		serverShutdownCtx, cancelServerShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		if err := server.Shutdown(serverShutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
		cancelServerShutdown()
		if err := manager.Shutdown(context.Background()); err != nil {
			log.Printf("stop updater operation: %v", err)
		}
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("serve updater API: %v", err)
		}
	}
}
