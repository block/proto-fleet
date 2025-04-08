package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/btc-mining/miner-firmware/fleet/generated/sqlc"
)

func WithTransaction[T any](ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) (T, error)) (T, error) {
	var zero T

	// TODO Which transaction isolation to use?
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return zero, fmt.Errorf("error opening tx %w", err)
	}

	defer tx.Rollback()

	sq := sqlc.New(tx)
	result, err := action(sq)
	if err != nil {
		return zero, err
	}

	err = tx.Commit()
	if err != nil {
		return zero, fmt.Errorf("error committing tx: %w", err)
	}

	return result, nil
}

func WithVoidTransaction(ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) error) error {
	_, err := WithTransaction(ctx, db, func(sq *sqlc.Queries) (any, error) {
		var emptyResult any

		return emptyResult, action(sq)
	})

	return err
}
