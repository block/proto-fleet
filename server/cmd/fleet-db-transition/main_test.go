package main

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCheckSourceArgumentsBeforeDatabaseConnection(t *testing.T) {
	t.Setenv("PGHOSTADDR", "")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"missing both", []string{"--state=source"}, "check --state=source requires --source"},
		{"missing source", []string{"--state=source", "--source-version=153"}, "check --state=source requires --source"},
		{"missing version", []string{"--state=source", "--source=public"}, "check --state=source requires --source-version"},
		{"source provided", []string{"--state=source", "--source=public", "--source-version=153"}, "begin baseline inspection: context canceled"},
		{"version policy remains in database check", []string{"--state=source", "--source=public", "--source-version=0"}, "begin baseline inspection: context canceled"},
		{"target needs no source", []string{"--state=target"}, "begin baseline inspection: context canceled"},
		{"startup needs no source", []string{"--state=startup"}, "begin baseline inspection: context canceled"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Cancellation exposes any attempt to inspect the database before
			// reporting missing source arguments, without a live database.
			args := append([]string{"--db-explicit-dsn=postgres://fleet@127.0.0.1:1/fleet?sslmode=disable", "check"}, tt.args...)
			err := run(ctx, args)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("run() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestMigrateTerminatesOnSignal(t *testing.T) {
	const helper = "FLEET_DB_TRANSITION_SIGNAL_TEST"
	if os.Getenv(helper) == "1" {
		os.Args = []string{os.Args[0], "migrate"}
		main()
		return
	}
	// An unserved local socket exercises connection retries without a database.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMigrateTerminatesOnSignal$") //nolint:gosec // Reexecute this test binary.
			for _, value := range os.Environ() {
				if !strings.HasPrefix(value, "DB_") && !strings.HasPrefix(value, "PG") {
					cmd.Env = append(cmd.Env, value)
				}
			}
			cmd.Env = append(cmd.Env, helper+"=1", "DB_ADDRESS="+listener.Addr().String(), "DB_INITIAL_CONNECTION_TIMEOUT=100ms")
			stderr, err := cmd.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill() }()
			scanner := bufio.NewScanner(stderr)
			ready := false
			for scanner.Scan() {
				if strings.Contains(scanner.Text(), "database not ready, retrying") {
					ready = true
					break
				}
			}
			if ready {
				if err = cmd.Process.Signal(sig); err != nil {
					t.Error(err)
				}
			}
			err = cmd.Wait()
			var exitErr *exec.ExitError
			if !ready || !errors.As(err, &exitErr) {
				t.Fatalf("expected signal exit after reaching retries; ready=%v, exit=%v", ready, err)
			}
			status, ok := exitErr.Sys().(syscall.WaitStatus)
			if !ok || !status.Signaled() || status.Signal() != sig {
				t.Fatalf("migration ignored %v: %v", sig, err)
			}
		})
	}
}
