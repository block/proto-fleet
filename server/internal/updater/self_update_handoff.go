package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const (
	selfUpdateHandoffSuffix     = ".handoff"
	selfUpdateHandoffTempSuffix = ".handoff.tmp"
	selfUpdateRestoreTempSuffix = ".restore"
	maxSelfUpdateHandoffBytes   = 4096
)

// ErrInterruptedSelfUpdateRestored tells the command to exit nonzero so its
// supervisor starts the executable that was restored before initialization.
var ErrInterruptedSelfUpdateRestored = errors.New("interrupted self-update restored; restart required")

var errRetriedSelfUpdateRestored = errors.New("retried self-update restored; continue with previous updater")

type selfUpdateHandoffMarker struct {
	ExecutablePath string `json:"executable_path"`
	Retry          bool   `json:"retry,omitempty"`
	Retried        bool   `json:"retried,omitempty"`
}

// SelfUpdateStartup is the one-shot authority carried by the first replacement
// process. A durable sibling marker preserves that authority if this process is
// interrupted before it binds the secured production socket.
type SelfUpdateStartup struct {
	executablePath string
	active         bool
}

// PrepareSelfUpdateStartup reconciles an interrupted replacement before the
// manager touches StateDir. A matching argv handoff identifies the first
// replacement attempt; a durable marker without that argv means a supervisor
// restarted an attempt that never reached readiness, so the previous binary is
// restored before initialization continues.
func PrepareSelfUpdateStartup(configuredPath, handoffPath string) (*SelfUpdateStartup, error) {
	if configuredPath != "" && !filepath.IsAbs(configuredPath) {
		return nil, fmt.Errorf("self-update path must be absolute")
	}
	if handoffPath != "" && !filepath.IsAbs(handoffPath) {
		return nil, fmt.Errorf("self-update handoff path must be absolute")
	}
	if configuredPath == "" {
		if handoffPath != "" {
			return nil, fmt.Errorf("self-update handoff provided without a configured executable path")
		}
		return nil, nil
	}
	canonicalPath, err := ensureTrustedSelfUpdateLocation(configuredPath)
	if err != nil {
		return nil, fmt.Errorf("validate self-update startup location: %w", err)
	}
	marker, exists, err := readSelfUpdateHandoffMarker(canonicalPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		if handoffPath != "" {
			return nil, fmt.Errorf("self-update handoff marker is missing")
		}
		if _, err := ensureTrustedSelfUpdatePath(configuredPath); err != nil {
			return nil, fmt.Errorf("validate self-update startup path: %w", err)
		}
		return nil, nil
	}
	if marker.ExecutablePath != canonicalPath {
		return nil, fmt.Errorf("self-update handoff marker path does not match configured executable")
	}
	if handoffPath != "" {
		validatedPath, err := ensureTrustedSelfUpdatePath(configuredPath)
		if err != nil {
			return nil, fmt.Errorf("validate replacement updater executable: %w", err)
		}
		if validatedPath != canonicalPath {
			return nil, fmt.Errorf("validated replacement path does not match durable marker")
		}
		canonicalHandoff, err := canonicalSelfUpdateDestination(handoffPath)
		if err != nil {
			return nil, fmt.Errorf("resolve self-update handoff argument: %w", err)
		}
		if canonicalHandoff != canonicalPath {
			return nil, fmt.Errorf("self-update handoff argument does not match durable marker")
		}
		return &SelfUpdateStartup{executablePath: canonicalPath, active: true}, nil
	}
	if marker.Retry {
		if _, err := ensureTrustedSelfUpdatePath(configuredPath); err != nil {
			return nil, fmt.Errorf("validate retried updater executable: %w", err)
		}
		marker.Retry = false
		marker.Retried = true
		if err := replaceSelfUpdateHandoffMarker(canonicalPath, marker); err != nil {
			return nil, fmt.Errorf("consume updater retry authority: %w", err)
		}
		return &SelfUpdateStartup{executablePath: canonicalPath, active: true}, nil
	}
	if err := rollbackPendingSelfUpdate(canonicalPath); err != nil {
		return nil, fmt.Errorf("restore interrupted self-update: %w", err)
	}
	if marker.Retried {
		return nil, errRetriedSelfUpdateRestored
	}
	return nil, ErrInterruptedSelfUpdateRestored
}

// Commit durably removes rollback eligibility after the real manager has
// initialized and the production socket has been bound and secured.
func (startup *SelfUpdateStartup) Commit() error {
	if startup == nil || !startup.active {
		return nil
	}
	if err := clearSelfUpdateHandoffMarker(startup.executablePath); err != nil {
		return err
	}
	startup.active = false
	return nil
}

// Rollback atomically restores the retained executable without consuming the
// backup, then durably clears the marker. Retaining the hard link makes the
// operation idempotent across a crash between restoration and marker removal.
func (startup *SelfUpdateStartup) Rollback() error {
	if startup == nil || !startup.active {
		return nil
	}
	_, markerExists, err := readSelfUpdateHandoffMarker(startup.executablePath)
	if err != nil {
		return err
	}
	if markerExists {
		err = rollbackPendingSelfUpdate(startup.executablePath)
	} else {
		// Commit may have unlinked the marker before its parent fsync failed.
		// The in-memory token still authorizes a conservative restoration.
		err = restorePreviousExecutable(startup.executablePath)
	}
	if err != nil {
		return err
	}
	startup.active = false
	return nil
}

func writeSelfUpdateHandoffMarker(destination string) error {
	canonicalDestination, err := canonicalSelfUpdateDestination(destination)
	if err != nil {
		return err
	}
	destination = canonicalDestination
	destinationInfo, err := os.Lstat(destination)
	if err != nil {
		return fmt.Errorf("inspect updater destination before handoff: %w", err)
	}
	backupInfo, err := os.Lstat(destination + selfUpdateBackupSuffix)
	if err != nil {
		return fmt.Errorf("inspect updater backup before handoff: %w", err)
	}
	if !os.SameFile(destinationInfo, backupInfo) {
		return fmt.Errorf("updater backup does not retain the current executable")
	}

	return persistSelfUpdateHandoffMarker(
		destination,
		selfUpdateHandoffMarker{ExecutablePath: destination},
		false,
	)
}

func authorizeSelfUpdateRestart(destination string) error {
	canonicalDestination, err := canonicalSelfUpdateDestination(destination)
	if err != nil {
		return err
	}
	marker, exists, err := readSelfUpdateHandoffMarker(canonicalDestination)
	if err != nil {
		return err
	}
	if !exists || marker.ExecutablePath != canonicalDestination {
		return fmt.Errorf("self-update handoff marker does not match executable")
	}
	marker.Retry = true
	marker.Retried = false
	return replaceSelfUpdateHandoffMarker(canonicalDestination, marker)
}

func replaceSelfUpdateHandoffMarker(destination string, marker selfUpdateHandoffMarker) error {
	return persistSelfUpdateHandoffMarker(destination, marker, true)
}

func persistSelfUpdateHandoffMarker(destination string, marker selfUpdateHandoffMarker, replace bool) error {
	canonicalDestination, err := canonicalSelfUpdateDestination(destination)
	if err != nil {
		return err
	}
	destination = canonicalDestination
	markerPath := destination + selfUpdateHandoffSuffix
	if _, err := os.Lstat(markerPath); err == nil && !replace {
		return fmt.Errorf("self-update handoff marker already exists")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect self-update handoff marker: %w", err)
	}
	tempPath := destination + selfUpdateHandoffTempSuffix
	if removed, err := unlinkExecutableSlot(tempPath); err != nil {
		return fmt.Errorf("clean stale self-update handoff temp file: %w", err)
	} else if removed {
		if err := syncDirectory(filepath.Dir(destination)); err != nil {
			return fmt.Errorf("persist stale handoff temp cleanup: %w", err)
		}
	}

	data, err := json.Marshal(marker)
	if err != nil {
		return fmt.Errorf("encode self-update handoff marker: %w", err)
	}
	fd, err := syscall.Open(
		tempPath,
		syscall.O_CREAT|syscall.O_EXCL|syscall.O_WRONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("create self-update handoff temp file: %w", err)
	}
	file := os.NewFile(uintptr(fd), tempPath)
	if file == nil {
		_ = syscall.Close(fd)
		return fmt.Errorf("create self-update handoff temp file")
	}
	installed := false
	defer func() {
		if !installed {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write self-update handoff marker: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync self-update handoff marker: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close self-update handoff marker: %w", err)
	}
	if err := os.Rename(tempPath, markerPath); err != nil {
		return fmt.Errorf("install self-update handoff marker: %w", err)
	}
	installed = true
	if err := syncDirectory(filepath.Dir(destination)); err != nil {
		return fmt.Errorf("persist self-update handoff marker: %w", err)
	}
	return nil
}

func readSelfUpdateHandoffMarker(destination string) (selfUpdateHandoffMarker, bool, error) {
	canonicalDestination, err := canonicalSelfUpdateDestination(destination)
	if err != nil {
		return selfUpdateHandoffMarker{}, false, err
	}
	destination = canonicalDestination
	markerPath := destination + selfUpdateHandoffSuffix
	fd, err := syscall.Open(markerPath, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return selfUpdateHandoffMarker{}, false, nil
	}
	if err != nil {
		return selfUpdateHandoffMarker{}, false, fmt.Errorf("open self-update handoff marker: %w", err)
	}
	file := os.NewFile(uintptr(fd), markerPath)
	if file == nil {
		_ = syscall.Close(fd)
		return selfUpdateHandoffMarker{}, false, fmt.Errorf("open self-update handoff marker")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return selfUpdateHandoffMarker{}, false, fmt.Errorf("inspect self-update handoff marker: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return selfUpdateHandoffMarker{}, false, fmt.Errorf("self-update handoff marker must be a protected regular file")
	}
	if err := validateDaemonPathOwner(markerPath, info); err != nil {
		return selfUpdateHandoffMarker{}, false, err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSelfUpdateHandoffBytes+1))
	if err != nil {
		return selfUpdateHandoffMarker{}, false, fmt.Errorf("read self-update handoff marker: %w", err)
	}
	if len(data) > maxSelfUpdateHandoffBytes {
		return selfUpdateHandoffMarker{}, false, fmt.Errorf("self-update handoff marker exceeds %d bytes", maxSelfUpdateHandoffBytes)
	}
	var marker selfUpdateHandoffMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return selfUpdateHandoffMarker{}, false, fmt.Errorf("decode self-update handoff marker: %w", err)
	}
	if marker.ExecutablePath == "" || !filepath.IsAbs(marker.ExecutablePath) || filepath.Clean(marker.ExecutablePath) != marker.ExecutablePath {
		return selfUpdateHandoffMarker{}, false, fmt.Errorf("self-update handoff marker contains an invalid executable path")
	}
	return marker, true, nil
}

func rollbackPendingSelfUpdate(destination string) error {
	canonicalDestination, err := canonicalSelfUpdateDestination(destination)
	if err != nil {
		return err
	}
	destination = canonicalDestination
	marker, exists, err := readSelfUpdateHandoffMarker(destination)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("self-update handoff marker is missing")
	}
	if marker.ExecutablePath != destination {
		return fmt.Errorf("self-update handoff marker path does not match executable")
	}
	if err := restorePreviousExecutable(destination); err != nil {
		return err
	}
	return clearSelfUpdateHandoffMarker(destination)
}

func clearSelfUpdateHandoffMarker(destination string) error {
	canonicalDestination, err := canonicalSelfUpdateDestination(destination)
	if err != nil {
		return err
	}
	destination = canonicalDestination
	marker, exists, err := readSelfUpdateHandoffMarker(destination)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("self-update handoff marker is missing")
	}
	if marker.ExecutablePath != destination {
		return fmt.Errorf("self-update handoff marker path does not match executable")
	}
	if err := syscall.Unlink(destination + selfUpdateHandoffSuffix); err != nil {
		return fmt.Errorf("remove self-update handoff marker: %w", err)
	}
	if err := syncDirectory(filepath.Dir(destination)); err != nil {
		return fmt.Errorf("persist self-update handoff marker removal: %w", err)
	}
	return nil
}

func canonicalSelfUpdateDestination(destination string) (string, error) {
	canonicalPath, err := filepath.EvalSymlinks(destination)
	if err == nil {
		return canonicalPath, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("resolve self-update executable path: %w", err)
	}
	canonicalParent, parentErr := filepath.EvalSymlinks(filepath.Dir(destination))
	if parentErr != nil {
		return "", fmt.Errorf("resolve self-update executable parent: %w", parentErr)
	}
	return filepath.Join(canonicalParent, filepath.Base(destination)), nil
}
