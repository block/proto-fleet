package bootstrap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type State struct {
	ServerURL              string    `yaml:"server_url"`
	AllowInsecureTransport bool      `yaml:"allow_insecure_transport,omitempty"`
	FleetNodeID            int64     `yaml:"fleet_node_id"`
	IdentityFingerprint    string    `yaml:"identity_fingerprint"`
	IdentityPrivateKeyHex  string    `yaml:"identity_private_key_hex"`
	IdentityPublicKeyHex   string    `yaml:"identity_public_key_hex"`
	CredentialKeyHex       string    `yaml:"credential_key_hex,omitempty"`
	APIKey                 string    `yaml:"api_key,omitempty"`
	SessionToken           string    `yaml:"session_token,omitempty"`
	SessionExpiresAt       time.Time `yaml:"session_expires_at,omitempty"`
}

func ResolveStateDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "fleetnode"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "fleetnode"), nil
}

func StatePath(dir string) string {
	return filepath.Join(dir, "state.yaml")
}

// A missing file is not an error: returns (zeroState, false, nil).
func LoadState(path string) (*State, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &State{}, false, nil
		}
		return nil, false, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, false, fmt.Errorf("parse state: %w", err)
	}
	return &s, true, nil
}

// Writes via temp + fsync + rename + dir-fsync so a power loss can't leave
// state.yaml truncated nor roll back a rename whose page-cache update never
// reached disk. Refuses to follow a symlink at the state-dir leaf so an
// attacker can't redirect credential writes via a hijacked path.
func SaveState(path string, s *State) error {
	dir := filepath.Dir(path)
	if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("state dir %s is a symlink; refusing to write secrets through it", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	if err := tightenStateDirPerms(dir); err != nil {
		return err
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "state-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename state: %w", err)
	}
	if err := syncDir(dir); err != nil {
		return err
	}
	return nil
}

// os.MkdirAll skips chmod on existing dirs, so a pre-existing 0755 dir would
// otherwise leave the credential file's enclosing path world-listable.
func tightenStateDirPerms(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat state dir: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // directory must keep owner-execute to be traversable
			return fmt.Errorf("chmod state dir: %w", err)
		}
	}
	return nil
}
