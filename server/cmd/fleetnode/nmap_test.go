//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveNmapPath_Default_BinaryAdjacent(t *testing.T) {
	// Arrange
	exeDir := t.TempDir()
	candidate := filepath.Join(exeDir, "nmap")
	require.NoError(t, os.WriteFile(candidate, []byte("#!/bin/sh\n"), 0o755))

	// Act
	got := resolveNmapPath(exeDir)

	// Assert
	assert.Equal(t, candidate, got)
}

func TestResolveNmapPath_Default_FallsBackToPATH(t *testing.T) {
	// Arrange
	exeDir := t.TempDir() // empty

	// Act
	got := resolveNmapPath(exeDir)

	// Assert
	assert.Equal(t, "nmap", got)
}

func TestResolveNmapPath_Default_AdjacentNotExecutableFallsBack(t *testing.T) {
	// Arrange
	exeDir := t.TempDir()
	candidate := filepath.Join(exeDir, "nmap")
	require.NoError(t, os.WriteFile(candidate, []byte("nope"), 0o644))

	// Act
	got := resolveNmapPath(exeDir)

	// Assert
	assert.Equal(t, "nmap", got)
}

func TestResolveNmapPath_EmptyExeDir(t *testing.T) {
	// Act
	got := resolveNmapPath("")

	// Assert
	assert.Equal(t, "nmap", got)
}

func TestValidateNmapTarget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty", input: "", wantErr: true},
		{name: "ipv4", input: "10.0.0.1", wantErr: false},
		{name: "ipv6", input: "2001:db8::1", wantErr: false},
		{name: "ipv4 cidr", input: "10.0.0.0/24", wantErr: false},
		{name: "ipv6 cidr", input: "2001:db8::/32", wantErr: false},
		{name: "ipv4 range", input: "10.0.0.1-50", wantErr: false},
		{name: "hostname", input: "miner-01.lan", wantErr: false},
		{name: "hostname single label", input: "miner", wantErr: false},
		{name: "leading dash flag", input: "-iL/etc/passwd", wantErr: true},
		{name: "nmap output flag", input: "-oN/tmp/loot", wantErr: true},
		{name: "embedded space", input: "10.0.0.1 -oN/tmp/x", wantErr: true},
		{name: "embedded null", input: "10.0.0.1\x00", wantErr: true},
		{name: "ipv6 with brackets", input: "[2001:db8::1]", wantErr: true},
		{name: "ipv4 range upper bound too high", input: "10.0.0.1-300", wantErr: true},
		{name: "ipv4 range bad head", input: "10.0.0.999-50", wantErr: true},
		{name: "shell metacharacter semicolon", input: "10.0.0.1;rm", wantErr: true},
		{name: "shell metacharacter ampersand", input: "10.0.0.1&touch", wantErr: true},
		{name: "leading whitespace", input: " 10.0.0.1", wantErr: true},
		{name: "trailing newline", input: "10.0.0.1\n", wantErr: true},
		{name: "hostname starts with dash", input: "-bad.lan", wantErr: true},
		{name: "hostname with underscore", input: "bad_name", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Act
			err := validateNmapTarget(tc.input)

			// Assert
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
