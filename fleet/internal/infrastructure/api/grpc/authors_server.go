package grpc

import (
	"context"
	"time"

	"connectrpc.com/connect"
	authorsv1 "github.com/btc-mining/miner-firmware/fleet/generated/grpc/authors/v1"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/authors/v1/authorsv1connect"
	"github.com/btc-mining/miner-firmware/fleet/internal/application"
	"github.com/btc-mining/miner-firmware/fleet/internal/domain"
)

type AuthorsServer struct {
	authorUseCases *application.AuthorUseCases
}

var _ authorsv1connect.AuthorsServiceHandler = &AuthorsServer{}

func NewAuthorsServer(authorUseCases *application.AuthorUseCases) *AuthorsServer {
	return &AuthorsServer{authorUseCases: authorUseCases}
}

func (s *AuthorsServer) Add(ctx context.Context, req *connect.Request[authorsv1.AddRequest]) (*connect.Response[authorsv1.AddResponse], error) {
	entity, err := s.authorUseCases.Create(ctx, req.Msg.Name, req.Msg.Bio)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&authorsv1.AddResponse{
		Author: authorToDto(entity),
	}), nil
}

func (s *AuthorsServer) List(ctx context.Context, _ *connect.Request[authorsv1.ListRequest]) (*connect.Response[authorsv1.ListResponse], error) {
	authors, err := s.authorUseCases.FindAll(ctx)
	if err != nil {
		return nil, err
	}

	resp := &authorsv1.ListResponse{}

	for _, entity := range authors {
		resp.Authors = append(resp.Authors, authorToDto(entity))
	}

	return connect.NewResponse(resp), nil
}

func authorToDto(entity *domain.Author) *authorsv1.Author {
	return &authorsv1.Author{
		Name:      entity.Name,
		Bio:       entity.Bio,
		CreatedAt: entity.CreatedAt.Format(time.RFC3339),
	}
}
