package application

import (
	"context"
	"database/sql"

	"github.com/btc-mining/miner-firmware/fleet/generated/sqlc"
	"github.com/btc-mining/miner-firmware/fleet/internal/domain"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/db"
)

type AuthUseCases struct {
	db          *sql.DB
	authUserSvc *domain.AuthService
}

func NewAuthUseCases(db *sql.DB, authUserSvc *domain.AuthService) *AuthUseCases {
	return &AuthUseCases{
		db:          db,
		authUserSvc: authUserSvc,
	}
}

func (uc AuthUseCases) AuthenticateUser(ctx context.Context, username string, password string) (domain.Token, error) {
	return db.WithTransaction(ctx, uc.db, func(sq *sqlc.Queries) (domain.Token, error) {
		return uc.authUserSvc.AuthenticateUser(ctx, sq, &domain.AuthenticateUserRequest{
			Username: username,
			Password: password,
		})
	})
}

func (uc AuthUseCases) CreateAdminUser(ctx context.Context, username string, password string) (domain.UserID, error) {
	return db.WithTransaction(ctx, uc.db, func(sq *sqlc.Queries) (domain.UserID, error) {
		return uc.authUserSvc.CreateAdminUser(ctx, sq, &domain.CreateAdminUserRequest{
			Username: username,
			Password: password,
		})
	})
}
