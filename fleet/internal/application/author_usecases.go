package application

import (
	"context"
	"database/sql"

	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/db"

	"github.com/btc-mining/miner-firmware/fleet/internal/domain"
)

type AuthorUseCases struct {
	db *sql.DB
}

func NewAuthorUseCases(db *sql.DB) *AuthorUseCases {
	return &AuthorUseCases{
		db: db,
	}
}

func (uc AuthorUseCases) Create(ctx context.Context, name string, bio string) (*domain.Author, error) {
	return db.WithTransaction(ctx, uc.db, func(tx *sql.Tx) (*domain.Author, error) {
		return domain.CreateAuthor(ctx, tx, &domain.CreateAuthorRequest{
			Name: name,
			Bio:  bio,
		})
	})
}

func (uc AuthorUseCases) FindAll(ctx context.Context) ([]*domain.Author, error) {
	return db.WithTransaction(ctx, uc.db, func(tx *sql.Tx) ([]*domain.Author, error) {
		return domain.FindAllAuthors(ctx, tx)
	})
}
