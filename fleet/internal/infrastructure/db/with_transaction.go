package db

import (
	"context"
	"database/sql"
	"fmt"
)

func WithTransaction[T any](ctx context.Context, db *sql.DB, action func(tx *sql.Tx) (T, error)) (T, error) {
	var zero T

	// TODO Which transaction isolation to use?
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return zero, fmt.Errorf("error opening tx %w", err)
	}

	defer tx.Rollback()

	result, err := action(tx)
	if err != nil {
		return zero, err
	}

	err = tx.Commit()
	if err != nil {
		return zero, fmt.Errorf("error committing tx: %w", err)
	}

	return result, nil
}

func WithVoidTransaction(ctx context.Context, db *sql.DB, action func(tx *sql.Tx) error) error {
	_, err := WithTransaction(ctx, db, func(tx *sql.Tx) (any, error) {
		var emptyResult any

		return emptyResult, action(tx)
	})

	return err
}
