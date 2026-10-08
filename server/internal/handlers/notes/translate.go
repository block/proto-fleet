package notes

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/notes/v1"
	"github.com/block/proto-fleet/server/internal/domain/notes/models"
)

func toProtoNote(n *models.Note) *pb.Note {
	return &pb.Note{
		Id:             n.ID,
		Content:        n.Content,
		AuthorUsername: n.AuthorUsername,
		CreatedAt:      timestamppb.New(n.CreatedAt),
		UpdatedAt:      timestamppb.New(n.UpdatedAt),
	}
}

func toListNotesResponse(rows []models.Note, nextPageToken string) *pb.ListNotesResponse {
	out := make([]*pb.Note, len(rows))
	for i := range rows {
		out[i] = toProtoNote(&rows[i])
	}
	return &pb.ListNotesResponse{Notes: out, NextPageToken: nextPageToken}
}
