package fleetmanagement

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
)

type Service struct {
	conn *sql.DB
}

func NewService(conn *sql.DB) *Service {
	return &Service{
		conn: conn,
	}
}

func (s *Service) UpdateDefaultPool(_ context.Context, r *pb.SetDefaultPoolRequest) error {
	slog.Debug("updating pool", "url", r.PoolConfig.Url, "username", r.PoolConfig.Username)

	// TODO actually store default pool in db
	return nil
}

func (s *Service) ListPairedMiners(c context.Context, req *pb.ListPairedMinersRequest) (*pb.ListPairedMinersResponse, error) {
	// Validate and set page size
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 50 // default page size
	}
	if pageSize > 1000 {
		pageSize = 1000 // maximum page size
	}

	// Decode cursor if provided
	cursor, err := decodeCursor(req.Cursor)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid page token: %w", err))
	}

	// Prepare query parameters
	params := sqlc.ListPairedDevicesParams{
		CursorID:       sql.NullInt64{Int64: cursor.ID, Valid: cursor.ID > 0},
		DeviceCursorID: sql.NullInt64{Int64: cursor.DeviceID, Valid: cursor.DeviceID > 0},
		Limit:          pageSize + 1, // request one extra to determine if there are more pages
	}

	return db.WithTransaction(c, s.conn, func(q *sqlc.Queries) (*pb.ListPairedMinersResponse, error) {

		// Query the database
		devices, err := q.ListPairedDevices(c, params)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list miners: %w", err))
		}

		// Prepare response
		resp := &pb.ListPairedMinersResponse{}

		// Handle pagination
		if len(devices) > int(pageSize) {
			// We got an extra record, so there are more pages
			resp.Miners = make([]*pb.PairedDevice, pageSize)
			for i, d := range devices[:pageSize] {
				resp.Miners[i] = &pb.PairedDevice{
					DeviceIdentifier: d.DeviceIdentifier,
					MacAddress:       d.MacAddress,
					SerialNumber:     d.SerialNumber.String,
				}
			}

			// Create next page token from last visible item
			lastDevice := devices[pageSize-1]
			cursor = Cursor{
				ID:       lastDevice.CursorID,
				DeviceID: lastDevice.DeviceID,
			}
			resp.Cursor = encodeCursor(cursor)
		} else {
			// This is the last page
			resp.Miners = make([]*pb.PairedDevice, len(devices))
			for i, d := range devices {
				resp.Miners[i] = &pb.PairedDevice{
					DeviceIdentifier: d.DeviceIdentifier,
					MacAddress:       d.MacAddress,
					SerialNumber:     d.SerialNumber.String,
				}
			}
		}

		// Get total count
		total, err := q.GetTotalPairedDevices(c)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get total count: %w", err))
		}
		resp.TotalMiners = int32(total) //nolint:gosec
		return resp, nil

	})
}

type Cursor struct {
	ID       int64
	DeviceID int64
}

func encodeCursor(c Cursor) string {
	raw := fmt.Sprintf("%d:%d", c.ID, c.DeviceID)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(encoded string) (Cursor, error) {
	if encoded == "" {
		return Cursor{}, nil
	}

	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Cursor{}, fmt.Errorf("invalid cursor encoding: %w", err)
	}

	var cursor Cursor
	_, err = fmt.Sscanf(string(b), "%d:%d", &cursor.ID, &cursor.DeviceID)
	if err != nil {
		return Cursor{}, fmt.Errorf("invalid cursor values: %w", err)
	}

	return cursor, nil
}
