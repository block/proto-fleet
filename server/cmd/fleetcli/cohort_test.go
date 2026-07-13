package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	cohortv1 "github.com/block/proto-fleet/server/generated/grpc/cohort/v1"
	"github.com/urfave/cli/v3"
)

func TestCohortReserveCountBuildsServerSelector(t *testing.T) {
	var req *cohortv1.CreateCohortRequest
	var buildErr error
	cmd := cohortReserveCommand()
	cmd.Action = func(ctx context.Context, cmd *cli.Command) error {
		req, buildErr = buildCreateCohortRequest(ctx, cmd, nil)
		return nil
	}

	err := cmd.Run(context.Background(), []string{
		"reserve",
		"--label", "reservation",
		"--purpose", "test",
		"--count", "2",
		"--product", " TestCorp ",
		"--model", " TestMiner ",
	})
	if err != nil {
		t.Fatalf("reserve flag harness error = %v", err)
	}
	if buildErr != nil {
		t.Fatalf("buildCreateCohortRequest error = %v", buildErr)
	}

	selector := req.GetSelect()
	if selector == nil {
		t.Fatalf("InitialMembers = %T, want selector", req.GetInitialMembers())
	}
	if selector.GetCount() != 2 {
		t.Fatalf("selector count = %d, want 2", selector.GetCount())
	}
	if selector.GetProduct() != "TestCorp" || selector.GetModel() != "TestMiner" {
		t.Fatalf("selector = %+v, want product/model filters", selector)
	}
}

func TestGeneratedCohortCreateConflictExitsTwo(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /cohort.v1.CohortService/CreateCohort", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"conflict"}`, http.StatusConflict)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	reqPath := filepath.Join(t.TempDir(), "create.json")
	body, err := json.Marshal(map[string]string{"label": "reservation", "purpose": "test"})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if err := os.WriteFile(reqPath, body, 0o600); err != nil {
		t.Fatalf("write request: %v", err)
	}

	err = newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"cohorts", "create", "--json", reqPath,
	})
	var exitErr cliExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("cohorts create error = %v, want cliExitError", err)
	}
	if exitErr.code != 2 {
		t.Fatalf("exit code = %d, want 2", exitErr.code)
	}
}

func TestCohortReserveConflictStillExitsTwo(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /cohort.v1.CohortService/CreateCohort", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"conflict"}`, http.StatusConflict)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"cohorts", "reserve",
		"--label", "reservation",
		"--purpose", "test",
		"--device", "miner-1",
	})
	var exitErr cliExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("cohorts reserve error = %v, want cliExitError", err)
	}
	if exitErr.code != 2 {
		t.Fatalf("exit code = %d, want 2", exitErr.code)
	}
}
