package updater

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/block/proto-fleet/server/internal/updaterapi"
)

const (
	preparedDirectoryName   = ".proto-fleet-prepared"
	preparedReceiptFilename = ".prepared.json"
	preparedUpdaterFilename = ".updater"
)

type preparedRelease struct {
	TargetVersion string `json:"target_version"`
	Repository    string `json:"repository"`
	SourceDigest  string `json:"source_digest"`
	ContentDigest string `json:"content_digest"`
}

func (m *Manager) capabilities() []string {
	if m.cfg.DeploymentMode == DeploymentModeHA {
		return []string{updaterapi.CapabilityPreparedUpdateV1}
	}
	return nil
}

// Only configuration is bound to the source; runtime data continues changing
// while the application serves traffic. Include the installed HA secrets and
// environment because HA Compose reads those outside the release tree.
func (m *Manager) preparedSourceDigest(ctx context.Context) (string, error) {
	current := filepath.Join(m.cfg.InstallRoot, "deployment")
	paths := []string{"version.txt", ".env", ".docker-daemon-id", "ssl", "server/influx_config/.env", "ha/node.env"}
	h := sha256.New()
	for _, name := range paths {
		digest, err := digestPreparedTree(ctx, filepath.Join(current, name), false)
		if err != nil {
			return "", fmt.Errorf("fingerprint installed configuration %s: %w", name, err)
		}
		_, _ = fmt.Fprintf(h, "%s:%s\n", name, digest)
	}
	digest, err := digestPreparedTree(ctx, filepath.Dir(haNodeEnvPath), false)
	if err != nil {
		return "", fmt.Errorf("fingerprint installed HA configuration: %w", err)
	}
	_, _ = fmt.Fprintln(h, digest)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Image archives are no longer consumed after preparation. Activation checks
// loaded images through Docker instead of reading large archives again.
func digestPreparedTree(ctx context.Context, root string, staged bool) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) && path == root {
			_, _ = io.WriteString(h, "absent\n")
			return nil
		}
		if walkErr != nil {
			return fmt.Errorf("walk prepared tree: %w", walkErr)
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("fingerprint prepared tree: %w", err)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("resolve prepared entry: %w", err)
		}
		if staged && relative == filepath.Join("deployment", "images") {
			return filepath.SkipDir
		}
		if staged && relative == preparedReceiptFilename {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect prepared entry: %w", err)
		}
		_, _ = fmt.Fprintf(h, "%s:%o\n", relative, info.Mode())
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("prepared entry must be a regular file: %s", path)
		}
		_, _ = fmt.Fprintf(h, "%d\n", info.Size())
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open prepared entry: %w", err)
		}
		_, copyErr := io.Copy(h, readerWithContext(ctx, file))
		closeErr := file.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return fmt.Errorf("fingerprint prepared entry: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("fingerprint release: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (m *Manager) publishPreparedRelease(ctx context.Context, stageRoot, targetVersion, sourceDigest string, updater *os.File) error {
	freshSource, err := m.preparedSourceDigest(ctx)
	if err != nil {
		return err
	}
	if freshSource != sourceDigest {
		return errors.New("installed configuration changed during preparation; prepare the release again")
	}
	if updater != nil {
		if _, err := updater.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("rewind prepared updater: %w", err)
		}
		candidate, err := os.OpenFile(filepath.Join(stageRoot, preparedUpdaterFilename), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("create durable prepared updater: %w", err)
		}
		_, copyErr := io.Copy(candidate, readerWithContext(ctx, updater))
		modeErr := candidate.Chmod(0700)
		syncErr := candidate.Sync()
		closeErr := candidate.Close()
		if err := errors.Join(copyErr, modeErr, syncErr, closeErr); err != nil {
			return fmt.Errorf("persist prepared updater: %w", err)
		}
	}
	digest, err := digestPreparedTree(ctx, stageRoot, true)
	if err != nil {
		return err
	}
	receipt := preparedRelease{TargetVersion: targetVersion, Repository: m.repository, SourceDigest: sourceDigest, ContentDigest: digest}
	data, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("encode prepared release receipt: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(stageRoot, preparedReceiptFilename), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("create prepared release receipt: %w", err)
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("persist prepared release receipt: %w", err)
	}
	if err := syncDirectory(stageRoot); err != nil {
		return err
	}
	preparedRoot := filepath.Join(m.cfg.InstallRoot, preparedDirectoryName)
	if err := os.RemoveAll(preparedRoot); err != nil {
		return fmt.Errorf("remove superseded prepared release: %w", err)
	}
	if err := os.Rename(stageRoot, preparedRoot); err != nil {
		return fmt.Errorf("publish prepared release: %w", err)
	}
	return syncDirectory(m.cfg.InstallRoot)
}

func (m *Manager) validatePreparedRelease(ctx context.Context, targetVersion string, verifyContent bool) error {
	root := filepath.Join(m.cfg.InstallRoot, preparedDirectoryName)
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("prepared release is unavailable; prepare %s before activation: %w", targetVersion, err)
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("prepared release directory is not private")
	}
	data, err := os.ReadFile(filepath.Join(root, preparedReceiptFilename))
	if err != nil {
		return fmt.Errorf("read prepared release receipt: %w", err)
	}
	var receipt preparedRelease
	if err := json.Unmarshal(data, &receipt); err != nil {
		return fmt.Errorf("decode prepared release receipt: %w", err)
	}
	if receipt.TargetVersion != targetVersion || receipt.Repository != m.repository {
		return errors.New("prepared release does not match requested target and repository; prepare it again")
	}
	source, err := m.preparedSourceDigest(ctx)
	if err != nil {
		return err
	}
	if source != receipt.SourceDigest {
		return errors.New("prepared release source configuration is stale; prepare it again")
	}
	if !verifyContent {
		return nil
	}
	digest, err := digestPreparedTree(ctx, root, true)
	if err != nil {
		return err
	}
	if digest != receipt.ContentDigest {
		return errors.New("prepared release content changed; prepare it again")
	}
	if err := validateStagedRelease(filepath.Join(root, "deployment"), targetVersion); err != nil {
		return err
	}
	return checkDeploymentRepository(filepath.Join(root, "deployment"), m.repository)
}

func (m *Manager) validatePreparedCapability(ctx context.Context, deployment string) error {
	var output bytes.Buffer
	bounded := newCappedWriter(&output, maxCandidateVersionBytes)
	if err := m.runHACommand(ctx, defaultCandidateTimeout, deployment, bounded, "update", "--capabilities"); err != nil {
		return fmt.Errorf("target release does not support prepared HA updates: %w", err)
	}
	if bounded.truncated {
		return errors.New("target prepared update capabilities exceeded output limit")
	}
	var response struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		return fmt.Errorf("decode target prepared update capabilities: %w", err)
	}
	if !slices.Contains(response.Capabilities, updaterapi.CapabilityPreparedUpdateV1) {
		return errors.New("target release does not support prepared-update-v1")
	}
	return nil
}
