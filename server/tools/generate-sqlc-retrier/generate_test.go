package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	t.Parallel()

	querierPath := filepath.Join(t.TempDir(), "querier.go")
	writeTestFile(t, querierPath, `package sqlc

import (
	"context"
	"database/sql"
)

type Widget struct{}

type Querier interface {
	DeleteWidget(ctx context.Context, id int64) error
	GetWidget(ctx context.Context, id int64) (Widget, error)
	ListWidgets(ctx context.Context) ([]Widget, error)
	TouchWidgets(ctx context.Context) (sql.Result, error)
	UpdateWidgets(ctx context.Context) (int64, error)
}
`)

	got, err := generate(querierPath)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	wantFragments := []string{
		"type QueryRetrier interface",
		"RetryQuery(ctx context.Context, operationName string, fn func() error) error",
		"func NewRetryingQuerier(next Querier, retrier QueryRetrier) Querier",
		`RetryQuery(ctx, "DeleteWidget", func() error`,
		`RetryQuery(ctx, "GetWidget", func() error`,
		`RetryQuery(ctx, "ListWidgets", func() error`,
		`RetryQuery(ctx, "TouchWidgets", func() error`,
		`RetryQuery(ctx, "UpdateWidgets", func() error`,
		"callResult, callErr := q.next.GetWidget(ctx, id)",
		"result = callResult",
		"return q.next.DeleteWidget(ctx, id)",
	}
	for _, fragment := range wantFragments {
		if !strings.Contains(string(got), fragment) {
			t.Errorf("generated output missing %q:\n%s", fragment, got)
		}
	}

	again, err := generate(querierPath)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if string(got) != string(again) {
		t.Fatal("generation is not deterministic")
	}
}

func TestGenerateRejectsUnsupportedReturnShape(t *testing.T) {
	t.Parallel()

	querierPath := filepath.Join(t.TempDir(), "querier.go")
	writeTestFile(t, querierPath, `package sqlc
import "context"
type Querier interface { Broken(ctx context.Context) int64 }
`)

	_, err := generate(querierPath)
	if err == nil || !strings.Contains(err.Error(), "unsupported return shape") {
		t.Fatalf("generate error = %v, want unsupported-return error", err)
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
