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
	"strings"
	"syscall"
	"time"

	"github.com/block/proto-fleet/server/internal/updater"
)

var version = "dev"

const selfUpdateHandoffFlag = "self-update-handoff"

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
	selfUpdateHandoff := flag.String(selfUpdateHandoffFlag, "", "Internal one-shot rollback path for a refreshed updater")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return nil
	}
	selfUpdateStartup, err := updater.PrepareSelfUpdateStartup(*selfUpdatePath, *selfUpdateHandoff)
	if err != nil {
		return fmt.Errorf("prepare updater startup: %w", err)
	}
	absoluteInstallRoot, err := filepath.Abs(*installRoot)
	if err != nil {
		return handleSelfUpdateStartupFailure(selfUpdateStartup, fmt.Errorf("resolve install root: %w", err))
	}
	manager, err := updater.NewManager(updater.Config{
		InstallRoot:    absoluteInstallRoot,
		StateDir:       *stateDir,
		SelfUpdatePath: *selfUpdatePath,
	})
	if err != nil {
		return handleSelfUpdateStartupFailure(selfUpdateStartup, fmt.Errorf("initialize updater: %w", err))
	}
	defer func() {
		if err := manager.Close(); err != nil {
			log.Printf("close updater manager: %v", err)
		}
	}()
	server := updater.NewServer(manager)
	errs := make(chan error, 1)
	go func() {
		errs <- server.Serve(*socketPath)
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	// A refreshed process remains rollback-eligible only until it proves real
	// startup with the production config/state and a bound, secured socket.
	// Signals are intentional shutdowns, not evidence that the candidate is bad.
	select {
	case <-server.Ready():
		if err := selfUpdateStartup.Commit(); err != nil {
			startupErr := fmt.Errorf("commit refreshed updater startup: %w", err)
			if shutdownErr := shutdownUpdater(server, manager); shutdownErr != nil {
				startupErr = errors.Join(startupErr, fmt.Errorf("shutdown after startup commit failure: %w", shutdownErr))
			}
			return handleSelfUpdateStartupFailure(selfUpdateStartup, startupErr)
		}
		log.Printf("proto-fleet-updater %s listening on %s", version, *socketPath)
	case sig := <-signals:
		log.Printf("received %s during startup, shutting down", sig)
		if err := shutdownUpdater(server, manager); err != nil {
			log.Printf("shutdown: %v", err)
		}
		return nil
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return handleSelfUpdateStartupFailure(selfUpdateStartup, fmt.Errorf("serve updater API: %w", err))
	}

	select {
	case sig := <-signals:
		log.Printf("received %s, shutting down", sig)
		if err := shutdownUpdater(server, manager); err != nil {
			log.Printf("shutdown: %v", err)
		}
		return nil
	case canonicalSelfUpdatePath := <-manager.SelfUpdateReady():
		log.Printf("activating refreshed host updater")
		if err := shutdownUpdater(server, manager); err != nil {
			return fmt.Errorf("drain updater before self-restart: %w", err)
		}
		if canonicalSelfUpdatePath == "" {
			return fmt.Errorf("self-update completed without a configured executable path")
		}
		execArgs := selfUpdateExecArgs(os.Args, canonicalSelfUpdatePath)
		if err := syscall.Exec(canonicalSelfUpdatePath, execArgs, os.Environ()); err != nil {
			rollbackErr := manager.RollbackSelfUpdate()
			if rollbackErr != nil {
				return errors.Join(
					fmt.Errorf("activate refreshed updater: %w", err),
					fmt.Errorf("restore previous updater after exec failure: %w", rollbackErr),
				)
			}
			return fmt.Errorf("activate refreshed updater; previous executable restored: %w", err)
		}
		return nil
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve updater API: %w", err)
		}
		return nil
	}
}

func handleSelfUpdateStartupFailure(selfUpdateStartup *updater.SelfUpdateStartup, startupErr error) error {
	if selfUpdateStartup == nil {
		return startupErr
	}
	if rollbackErr := selfUpdateStartup.Rollback(); rollbackErr != nil {
		return errors.Join(
			startupErr,
			fmt.Errorf("restore previous updater after replacement startup failure: %w", rollbackErr),
		)
	}
	return fmt.Errorf("start refreshed updater; previous executable restored: %w", startupErr)
}

func selfUpdateExecArgs(args []string, handoffPath string) []string {
	if len(args) == 0 {
		return []string{"proto-fleet-updater", "--" + selfUpdateHandoffFlag + "=" + handoffPath}
	}
	cleaned := make([]string, 0, len(args)+1)
	cleaned = append(cleaned, args[0], "--"+selfUpdateHandoffFlag+"="+handoffPath)
	for index := 1; index < len(args); index++ {
		arg := args[index]
		if arg == "--"+selfUpdateHandoffFlag || arg == "-"+selfUpdateHandoffFlag {
			index++
			continue
		}
		if strings.HasPrefix(arg, "--"+selfUpdateHandoffFlag+"=") || strings.HasPrefix(arg, "-"+selfUpdateHandoffFlag+"=") {
			continue
		}
		cleaned = append(cleaned, arg)
	}
	return cleaned
}

func shutdownUpdater(server *updater.Server, manager *updater.Manager) error {
	serverShutdownCtx, cancelServerShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	serverErr := server.Shutdown(serverShutdownCtx)
	cancelServerShutdown()
	managerErr := manager.Shutdown(context.Background())
	return errors.Join(serverErr, managerErr)
}
