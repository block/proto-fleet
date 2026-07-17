package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRetryDBExecResetsPoolOnFailoverWithoutRetry(t *testing.T) {
	pgErr := &pgconn.PgError{Code: PGConnectionFailure, Message: "connection failure"}
	sqlDB := sql.OpenDB(&haResetConnector{execErr: pgErr})
	defer sqlDB.Close()

	resets := 0
	RegisterPoolReset(sqlDB, func() {
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
	RegisterPoolReset(sqlDB, func() {
		resets++
	})

	_, err := WithTransaction(context.Background(), sqlDB, func(*sqlc.Queries) (struct{}, error) {
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
	RegisterPoolReset(sqlDB, func() {
		resets++
	})

	calls := 0
	_, err := WithTransaction(context.Background(), sqlDB, func(*sqlc.Queries) (struct{}, error) {
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

func TestWithTransactionCommitFailoverResetsPoolWithoutRetry(t *testing.T) {
	pgErr := &pgconn.PgError{Code: PGConnectionFailure, Message: "connection failure"}
	sqlDB := sql.OpenDB(&haResetConnector{commitErr: pgErr})
	defer sqlDB.Close()

	resets := 0
	RegisterPoolReset(sqlDB, func() {
		resets++
	})

	calls := 0
	_, err := WithTransaction(context.Background(), sqlDB, func(*sqlc.Queries) (struct{}, error) {
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

type haResetConnector struct {
	beginErr  error
	commitErr error
	execErr   error
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
	return haResetTx{commitErr: c.connector.commitErr}, nil
}

func (c *haResetConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	if c.connector.execErr != nil {
		return nil, c.connector.execErr
	}
	return haResetResult(0), nil
}

type haResetTx struct {
	commitErr error
}

func (t haResetTx) Commit() error {
	return t.commitErr
}

func (t haResetTx) Rollback() error {
	return nil
}

type haResetResult int64

func (r haResetResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (r haResetResult) RowsAffected() (int64, error) {
	return int64(r), nil
}
