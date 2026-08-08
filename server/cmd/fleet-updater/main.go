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
	"github.com/block/proto-fleet/server/internal/updaterapi"
)

var version = "dev"

const selfUpdateHandoffFlag = "self-update-handoff"

func main() {
	setSecureProcessUmask()
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func setSecureProcessUmask() {
	// Keep the Unix socket and updater-owned artifacts private from creation,
	// including when the binary is started outside the hardened systemd unit.
	// The umask is process-global, so establish it before run starts goroutines.
	syscall.Umask(0o077)
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
	defaultDeploymentMode := os.Getenv("PROTO_FLEET_UPDATER_DEPLOYMENT_MODE")
	if defaultDeploymentMode == "" {
		defaultDeploymentMode = string(updater.DeploymentModeStandalone)
	}

	installRoot := flag.String("install-root", defaultInstallRoot, "Proto Fleet installation root")
	stateDir := flag.String("state-dir", defaultStateDir, "Durable updater state directory")
	socketPath := flag.String("socket-path", defaultSocketPath, "Unix socket path")
	selfUpdatePath := flag.String("self-update-path", defaultSelfUpdatePath, "Installed updater binary path to atomically refresh")
	selfUpdateHandoff := flag.String(selfUpdateHandoffFlag, "", "Internal one-shot rollback path for a refreshed updater")
	deploymentMode := flag.String("deployment-mode", defaultDeploymentMode, "Deployment mode: standalone or ha")
	repairStartup := flag.Bool("repair-startup", false, "Repair an interrupted deployment layout and exit")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return nil
	}
	var selfUpdateStartup *updater.SelfUpdateStartup
	if !*repairStartup {
		var err error
		selfUpdateStartup, err = updater.PrepareSelfUpdateStartup(*selfUpdatePath, *selfUpdateHandoff)
		if err != nil {
			return fmt.Errorf("prepare updater startup: %w", err)
		}
	}
	absoluteInstallRoot, err := filepath.Abs(*installRoot)
	if err != nil {
		return handleSelfUpdateStartupFailure(selfUpdateStartup, fmt.Errorf("resolve install root: %w", err))
	}
	config := updater.Config{
		InstallRoot:         absoluteInstallRoot,
		StateDir:            *stateDir,
		SelfUpdatePath:      *selfUpdatePath,
		DeploymentMode:      updater.DeploymentMode(*deploymentMode),
		QualificationTarget: os.Getenv("PROTO_FLEET_HA_QUALIFICATION_TARGET"),
	}
	if *repairStartup {
		probeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		// A listening daemon already acquired the process lock and completed
		// startup repair before opening its socket.
		if _, err := updaterapi.NewClient(*socketPath).Status(probeCtx); err == nil {
			return nil
		}
		return updater.RepairStartup(config)
	}
	manager, err := updater.NewManager(config)
	if err != nil {
		return handleSelfUpdateStartupFailure(selfUpdateStartup, fmt.Errorf("initialize updater: %w", err))
	}
	defer func() {
		if err := manager.Close(); err != nil {
			log.Printf("close updater manager: %v", err)
		}
	}()
	if err := manager.RecoverApplication(); err != nil {
		return handleSelfUpdateStartupFailure(selfUpdateStartup, fmt.Errorf("recover interrupted HA application: %w", err))
	}
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
		rolledBackPath, err := rollbackSelfUpdateQueuedDuringShutdown(
			manager.SelfUpdateReady(),
			manager.RollbackSelfUpdate,
		)
		if err != nil {
			return fmt.Errorf("restore previous updater after shutdown interrupted self-restart: %w", err)
		}
		if rolledBackPath != "" {
			log.Printf(
				"host updater refresh completed during shutdown; restored the previous updater and deferred the refresh until a future upgrade",
			)
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
		// Shutdown may have waited for activation while an intentional stop was
		// queued. Stop channel forwarding before the final check so a later signal
		// uses its default disposition instead of being swallowed before exec.
		signal.Stop(signals)
		pendingSignal, err := rollbackSelfUpdateForPendingSignal(signals, manager.RollbackSelfUpdate)
		if err != nil {
			return fmt.Errorf("restore previous updater after shutdown interrupted self-restart: %w", err)
		}
		if pendingSignal != nil {
			log.Printf("received %s while preparing self-restart; restored the previous updater and shutting down", pendingSignal)
			return nil
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

// rollbackSelfUpdateQueuedDuringShutdown must run after Manager.Shutdown. At
// that point an activating operation has finished, so this non-blocking read
// cannot race a later self-update notification. A signal is an intentional
// stop, so restore the previous updater instead of execing a new process.
func rollbackSelfUpdateQueuedDuringShutdown(
	ready <-chan string,
	rollback func() error,
) (string, error) {
	select {
	case canonicalPath := <-ready:
		if canonicalPath == "" {
			return "", fmt.Errorf("self-update completed without a configured executable path")
		}
		if err := rollback(); err != nil {
			return canonicalPath, err
		}
		return canonicalPath, nil
	default:
		return "", nil
	}
}

func rollbackSelfUpdateForPendingSignal(
	signals <-chan os.Signal,
	rollback func() error,
) (os.Signal, error) {
	select {
	case pendingSignal := <-signals:
		if err := rollback(); err != nil {
			return pendingSignal, err
		}
		return pendingSignal, nil
	default:
		return nil, nil
	}
}
