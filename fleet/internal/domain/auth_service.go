package domain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/btc-mining/miner-firmware/fleet/generated/sqlc"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type AuthenticateUserRequest struct {
	Username string
	Password string
}

type CreateAdminUserRequest struct {
	Username string
	Password string
}

type Token string

type UserID string

var (
	ErrUserNotFound        = errors.New("user not found")
	ErrAdminAlreadyCreated = errors.New("admin already created")
	ErrInvalidInput        = errors.New("invalid input")
)

type AuthService struct {
	tokenSvc *TokenService
}

func NewAuthService(tokenSvc *TokenService) *AuthService {
	return &AuthService{
		tokenSvc: tokenSvc,
	}
}

func (s *AuthService) AuthenticateUser(ctx context.Context, q *sqlc.Queries, req *AuthenticateUserRequest) (Token, error) {
	// Fetch user from the database
	user, err := q.GetUserByUsername(ctx, req.Username)
	if err != nil {
		return "", err
	}

	// Compare hashed passwords
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return "", fmt.Errorf("error validating password: %w", err)
	}

	// Generate and return JWT token
	token, err := s.tokenSvc.GenerateJWT(user.UserID)
	if err != nil {
		return "", err
	}
	return Token(token), err
}

func (s *AuthService) CreateAdminUser(ctx context.Context, q *sqlc.Queries, req *CreateAdminUserRequest) (UserID, error) {
	if len(req.Username) == 0 || len(req.Password) == 0 {
		return "", ErrInvalidInput
	}
	// generate salted password hash
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("error generating password: %w", err)
	}
	userID := uuid.New().String()
	_, err = q.CreateUser(ctx, sqlc.CreateUserParams{
		UserID:       userID,
		Username:     req.Username,
		PasswordHash: string(hashedPassword),
		CreatedAt:    time.Now(),
	})
	if err != nil {
		return "", err
	}

	return UserID(userID), nil
}
