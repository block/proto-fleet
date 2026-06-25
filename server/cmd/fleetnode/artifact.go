package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

const commandArtifactTransferChunkSize = 1 << 20

func uploadCommandArtifact(ctx context.Context, client gatewayClient, header *pb.CommandArtifactUploadHeader, reader io.Reader) (*pb.CommandArtifactRef, error) {
	stream := client.UploadCommandArtifact(ctx)
	if err := stream.Send(&pb.UploadCommandArtifactRequest{Part: &pb.UploadCommandArtifactRequest_Header{Header: header}}); err != nil {
		return nil, fmt.Errorf("send command artifact header: %w", err)
	}

	buf := make([]byte, commandArtifactTransferChunkSize)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if err := stream.Send(&pb.UploadCommandArtifactRequest{Part: &pb.UploadCommandArtifactRequest_Chunk{Chunk: &pb.CommandArtifactChunk{Data: chunk}}}); err != nil {
				return nil, fmt.Errorf("send command artifact chunk: %w", err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read command artifact: %w", readErr)
		}
	}

	resp, err := stream.CloseAndReceive()
	if err != nil {
		return nil, fmt.Errorf("finish command artifact upload: %w", err)
	}
	return resp.Msg.GetArtifact(), nil
}

func downloadCommandArtifact(ctx context.Context, client gatewayClient, req *pb.DownloadCommandArtifactRequest, writer io.Writer) (*pb.CommandArtifactRef, error) {
	stream, err := client.DownloadCommandArtifact(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, fmt.Errorf("start command artifact download: %w", err)
	}
	defer stream.Close()

	var header *pb.CommandArtifactRef
	hasher := sha256.New()
	var written int64
	for stream.Receive() {
		msg := stream.Msg()
		if h := msg.GetHeader(); h != nil {
			if header != nil {
				return nil, fmt.Errorf("command artifact download sent duplicate header")
			}
			header = h.GetArtifact()
			continue
		}
		chunk := msg.GetChunk()
		if chunk == nil {
			return nil, fmt.Errorf("command artifact download sent empty message")
		}
		if header == nil {
			return nil, fmt.Errorf("command artifact download sent chunk before header")
		}
		data := chunk.GetData()
		if _, err := writer.Write(data); err != nil {
			return nil, fmt.Errorf("write command artifact chunk: %w", err)
		}
		if _, err := hasher.Write(data); err != nil {
			return nil, fmt.Errorf("hash command artifact chunk: %w", err)
		}
		written += int64(len(data))
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("receive command artifact download: %w", err)
	}
	if header == nil {
		return nil, fmt.Errorf("command artifact download ended before header")
	}
	if written != header.GetSizeBytes() {
		return nil, fmt.Errorf("command artifact download size mismatch: header declared %d bytes, received %d bytes", header.GetSizeBytes(), written)
	}
	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	if actualSHA != header.GetSha256() {
		return nil, fmt.Errorf("command artifact download sha256 mismatch")
	}
	return header, nil
}
