package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/btc-mining/miner-firmware/fleet/generated/sqlc"
)

type Author struct {
	ID        int64
	Name      string
	Bio       string
	CreatedAt time.Time
}

type CreateAuthorRequest struct {
	Name string
	Bio  string
}

func CreateAuthor(ctx context.Context, q *sqlc.Queries, author *CreateAuthorRequest) (*Author, error) {
	if author.Name == "" {
		return nil, errors.New("Author name is required")
	}
	result, err := q.CreateAuthor(ctx, sqlc.CreateAuthorParams{
		Name: author.Name,
		Bio: sql.NullString{
			String: author.Bio,
			Valid:  author.Bio != "",
		},
	})
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("error fetching LastInsertId: %w", err)
	}

	dbAuthor, err := q.FindAuthorByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Author{
		ID:        dbAuthor.ID,
		Name:      dbAuthor.Name,
		Bio:       dbAuthor.Bio.String,
		CreatedAt: dbAuthor.CreatedAt,
	}, nil
}

func FindAllAuthors(ctx context.Context, q *sqlc.Queries) ([]*Author, error) {
	rows, err := q.FindAllAuthors(ctx)
	if err != nil {
		return nil, err
	}
	var results []*Author
	for _, dbAuthor := range rows {
		results = append(results, &Author{
			ID:        dbAuthor.ID,
			Bio:       dbAuthor.Bio.String,
			Name:      dbAuthor.Name,
			CreatedAt: dbAuthor.CreatedAt,
		})
	}
	return results, nil
}

func UpdateAuthor(ctx context.Context, q *sqlc.Queries, author *Author) (*Author, error) {
	_, err := q.UpdateAuthor(ctx, sqlc.UpdateAuthorParams{
		ID:   author.ID,
		Name: author.Name,
		Bio: sql.NullString{
			String: author.Bio,
			Valid:  author.Bio != "",
		},
	})
	if err != nil {
		return nil, err
	}

	dbAuthor, err := q.FindAuthorByID(context.Background(), author.ID)
	if err != nil {
		return nil, err
	}
	return &Author{
		ID:        dbAuthor.ID,
		Bio:       dbAuthor.Bio.String,
		Name:      dbAuthor.Name,
		CreatedAt: dbAuthor.CreatedAt,
	}, nil
}
