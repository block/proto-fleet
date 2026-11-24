package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryEvidenceRejectsUnknownTrailingAndOversizedInput(t *testing.T) {
	for _, body := range []string{`{"unrecognized":true}`, `{} {}`, strings.Repeat(" ", 64<<10) + `{}`} {
		_, err := readEvidence(strings.NewReader(body))
		require.Error(t, err)
	}
	_, err := readEvidence(strings.NewReader(`{"operator":"host ops"}`))
	require.NoError(t, err)
}

func TestRecoveryApplyRequiresOfflineAcknowledgementBeforeDatabaseAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0600))
	err := run(context.Background(), []string{"resolve", "--evidence", path, "--apply"})
	require.ErrorContains(t, err, "--acknowledge-all-dispatchers-stopped")
}
