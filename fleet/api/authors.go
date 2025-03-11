package api

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"connectrpc.com/connect"

	"github.com/btc-mining/miner-firmware/fleet/api/gen/authors/v1"
	"github.com/btc-mining/miner-firmware/fleet/api/gen/authors/v1/authorsv1connect"
	"github.com/btc-mining/miner-firmware/fleet/db/sqlc"
)

type AuthorsServer struct {
	db *sql.DB
	q  *sqlc.Queries
}

var _ authorsv1connect.AuthorsServiceHandler = &AuthorsServer{}

func NewAuthorsServer(conn *sql.DB, q *sqlc.Queries) *AuthorsServer {
	return &AuthorsServer{db: conn, q: q}
}

func (s *AuthorsServer) Add(ctx context.Context, req *connect.Request[authorsv1.AddRequest]) (*connect.Response[authorsv1.AddResponse], error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("error opening tx %w", err)
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	result, err := q.
		CreateAuthor(ctx, sqlc.CreateAuthorParams{
			Name: req.Msg.Name,
			Bio: sql.NullString{
				String: req.Msg.Bio,
				Valid:  true,
			},
		})
	if err != nil {
		return nil, fmt.Errorf("error opening tx: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("error getting id: %w", err)
	}
	author, err := q.GetAuthor(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("error getting author: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("error committing tx: %w", err)
	}
	return connect.NewResponse(&authorsv1.AddResponse{
		Author: &authorsv1.Author{
			Name:      author.Name,
			Bio:       author.Bio.String,
			CreatedAt: author.CreatedAt.Format(time.RFC3339),
		},
	}), nil
}

func (s *AuthorsServer) List(ctx context.Context, _ *connect.Request[authorsv1.ListRequest]) (*connect.Response[authorsv1.ListResponse], error) {
	authors, err := s.q.ListAuthors(ctx)
	if err != nil {
		return nil, fmt.Errorf("error opening tx: %w", err)
	}
	resp := &authorsv1.ListResponse{}
	for _, author := range authors {
		resp.Authors = append(resp.Authors, &authorsv1.Author{
			Name:      author.Name,
			Bio:       author.Bio.String,
			CreatedAt: author.CreatedAt.Format(time.RFC3339),
		})
	}
	return connect.NewResponse(resp), nil
}
