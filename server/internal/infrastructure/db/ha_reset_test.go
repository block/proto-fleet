package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRetryDBExecResetsPoolOnFailoverWithoutRetry(t *testing.T) {
	pgErr := &pgconn.PgError{Code: PGConnectionFailure, Message: "connection failure"}
	sqlDB := sql.OpenDB(&haResetConnector{execErr: pgErr})
	defer sqlDB.Close()

	resets := 0
	registerPoolResetForTest(t, sqlDB, func() {
		resets++
	})

	_, err := NewRetryDB(sqlDB).ExecContext(context.Background(), "UPDATE device SET updated_at = now()")
	if !errors.Is(err, pgErr) {
		t.Fatalf("ExecContext error = %v, want wrapped failover error", err)
	}
	if resets != 1 {
		t.Fatalf("pool resets = %d, want 1", resets)
	}
}

func TestWithTransactionBeginFailoverResetsPoolWithoutRetry(t *testing.T) {
	pgErr := &pgconn.PgError{Code: PGConnectionFailure, Message: "connection failure"}
	sqlDB := sql.OpenDB(&haResetConnector{beginErr: pgErr})
	defer sqlDB.Close()

	resets := 0
	registerPoolResetForTest(t, sqlDB, func() {
		resets++
	})

	_, err := WithTransaction(context.Background(), sqlDB, func(sqlc.Querier) (struct{}, error) {
		t.Fatal("transaction action should not run")
		return struct{}{}, nil
	})
	if !IsFailoverPostgresError(err) {
		t.Fatalf("WithTransaction error = %v, want failover-class error", err)
	}
	if resets != 1 {
		t.Fatalf("pool resets = %d, want 1", resets)
	}
}

func TestWithTransactionActionFailoverResetsPoolWithoutRetry(t *testing.T) {
	pgErr := &pgconn.PgError{Code: PGConnectionFailure, Message: "connection failure"}
	sqlDB := sql.OpenDB(&haResetConnector{})
	defer sqlDB.Close()

	resets := 0
	registerPoolResetForTest(t, sqlDB, func() {
		resets++
	})

	calls := 0
	_, err := WithTransaction(context.Background(), sqlDB, func(sqlc.Querier) (struct{}, error) {
		calls++
		return struct{}{}, pgErr
	})
	if !errors.Is(err, pgErr) {
		t.Fatalf("WithTransaction error = %v, want wrapped failover error", err)
	}
	if calls != 1 {
		t.Fatalf("transaction action calls = %d, want 1", calls)
	}
	if resets != 1 {
		t.Fatalf("pool resets = %d, want 1", resets)
	}
}

func TestWithTransactionQueryFailoverResetsPoolAfterRollback(t *testing.T) {
	pgErr := &pgconn.PgError{Code: PGConnectionFailure, Message: "connection failure"}
	rolledBack := false
	resetAfterRollback := false
	sqlDB := sql.OpenDB(&haResetConnector{
		queryErr: pgErr,
		rollback: func() {
			rolledBack = true
		},
	})
	defer sqlDB.Close()

	resets := 0
	registerPoolResetForTest(t, sqlDB, func() {
		resets++
		if rolledBack {
			resetAfterRollback = true
		}
	})

	_, err := WithTransaction(context.Background(), sqlDB, func(q sqlc.Querier) (struct{}, error) {
		_, queryErr := q.GetSessionByID(context.Background(), "session")
		if !errors.Is(queryErr, pgErr) {
			t.Fatalf("query error = %v, want wrapped failover error", queryErr)
		}
		return struct{}{}, fmt.Errorf("store wrapped: %w", queryErr)
	})
	if !errors.Is(err, pgErr) {
		t.Fatalf("WithTransaction error = %v, want wrapped failover error", err)
	}
	if resets != 1 {
		t.Fatalf("pool resets = %d, want 1", resets)
	}
	if !resetAfterRollback {
		t.Fatal("pool reset happened before rollback")
	}
}

func TestFailoverResettingQuerierOverRetryDBResetsOnce(t *testing.T) {
	pgErr := &pgconn.PgError{Code: PGConnectionFailure, Message: "connection failure"}
	sqlDB := sql.OpenDB(&haResetConnector{queryErr: pgErr})
	defer sqlDB.Close()

	resets := 0
	registerPoolResetForTest(t, sqlDB, func() {
		resets++
	})

	_, err := NewFailoverResettingQuerier(NewRetryDB(sqlDB)).GetSessionByID(context.Background(), "session")
	if !errors.Is(err, pgErr) {
		t.Fatalf("query error = %v, want wrapped failover error", err)
	}
	if resets != 1 {
		t.Fatalf("pool resets = %d, want 1", resets)
	}
}

func TestWithTransactionCommitFailoverResetsPoolWithoutRetry(t *testing.T) {
	pgErr := &pgconn.PgError{Code: PGConnectionFailure, Message: "connection failure"}
	sqlDB := sql.OpenDB(&haResetConnector{commitErr: pgErr})
	defer sqlDB.Close()

	resets := 0
	registerPoolResetForTest(t, sqlDB, func() {
		resets++
	})

	calls := 0
	_, err := WithTransaction(context.Background(), sqlDB, func(sqlc.Querier) (struct{}, error) {
		calls++
		return struct{}{}, nil
	})
	if !IsFailoverPostgresError(err) {
		t.Fatalf("WithTransaction error = %v, want failover-class error", err)
	}
	if calls != 1 {
		t.Fatalf("transaction action calls = %d, want 1", calls)
	}
	if resets != 1 {
		t.Fatalf("pool resets = %d, want 1", resets)
	}
}

func TestWithTransactionNoRetryResetsPoolWithoutReplayingAction(t *testing.T) {
	for _, tc := range []struct {
		name      string
		phase     string
		err       error
		wantCalls int
		wantReset int
	}{
		{"begin failover", "begin", &pgconn.PgError{Code: PGConnectionFailure}, 0, 1},
		{"action failover", "action", &pgconn.PgError{Code: PGConnectionFailure}, 1, 1},
		{"query failover", "query", &pgconn.PgError{Code: PGConnectionFailure}, 1, 1},
		{"commit failover", "commit", &pgconn.PgError{Code: PGConnectionFailure}, 1, 1},
		{"retryable action error", "action", &pgconn.PgError{Code: PGSerializationFailure}, 1, 0},
		{"retryable commit error", "commit", &pgconn.PgError{Code: PGSerializationFailure}, 1, 0},
		{"ordinary action error", "action", errors.New("action failed"), 1, 0},
		{"success", "", nil, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rolledBack := false
			connector := &haResetConnector{rollback: func() { rolledBack = true }}
			switch tc.phase {
			case "begin":
				connector.beginErr = tc.err
			case "query":
				connector.queryErr = tc.err
			case "commit":
				connector.commitErr = tc.err
			}
			sqlDB := sql.OpenDB(connector)
			defer sqlDB.Close()
			resets := 0
			registerPoolResetForTest(t, sqlDB, func() {
				resets++
				if (tc.phase == "action" || tc.phase == "query") && !rolledBack {
					t.Error("pool reset happened before rollback")
				}
			})
			calls := 0
			result, err := WithTransactionNoRetry(t.Context(), sqlDB, func(q sqlc.Querier) (int, error) {
				calls++
				switch tc.phase {
				case "action":
					return 0, tc.err
				case "query":
					_, queryErr := q.GetSessionByID(t.Context(), "session")
					return 0, fmt.Errorf("store wrapped: %w", queryErr)
				default:
					return 42, nil
				}
			})
			if !errors.Is(err, tc.err) {
				t.Errorf("WithTransactionNoRetry error = %v, want %v", err, tc.err)
			}
			if tc.err == nil && result != 42 {
				t.Errorf("result = %d, want 42", result)
			}
			if calls != tc.wantCalls || resets != tc.wantReset {
				t.Errorf("action calls = %d, resets = %d; want %d, %d", calls, resets, tc.wantCalls, tc.wantReset)
			}
		})
	}
}

type haResetConnector struct {
	beginErr  error
	commitErr error
	execErr   error
	queryErr  error
	rollback  func()
}

func (c *haResetConnector) Connect(context.Context) (driver.Conn, error) {
	return &haResetConn{connector: c}, nil
}

func (c *haResetConnector) Driver() driver.Driver {
	return haResetDriver{connector: c}
}

type haResetDriver struct {
	connector *haResetConnector
}

func (d haResetDriver) Open(string) (driver.Conn, error) {
	return &haResetConn{connector: d.connector}, nil
}

type haResetConn struct {
	connector *haResetConnector
}

func (c *haResetConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not implemented")
}

func (c *haResetConn) Close() error {
	return nil
}

func (c *haResetConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *haResetConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	if c.connector.beginErr != nil {
		return nil, c.connector.beginErr
	}
	return haResetTx{commitErr: c.connector.commitErr, rollback: c.connector.rollback}, nil
}

func (c *haResetConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	if c.connector.execErr != nil {
		return nil, c.connector.execErr
	}
	return driver.RowsAffected(0), nil
}

func (c *haResetConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	if c.connector.queryErr != nil {
		return nil, c.connector.queryErr
	}
	return nil, errors.New("query not implemented")
}

type haResetTx struct {
	commitErr error
	rollback  func()
}

func (t haResetTx) Commit() error {
	return t.commitErr
}

func (t haResetTx) Rollback() error {
	if t.rollback != nil {
		t.rollback()
	}
	return nil
}

func registerPoolResetForTest(t *testing.T, conn *sql.DB, reset func()) {
	t.Helper()
	registerPoolReset(conn, reset)
	t.Cleanup(func() {
		poolResetRegistry.Delete(conn)
	})
}
