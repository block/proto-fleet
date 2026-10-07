package fleetinventory

import (
	"context"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func (s *Service) TankOptions(ctx context.Context, orgID int64, req *pb.ListTankOptionsRequest, scopes []Scope) (*pb.ListTankOptionsResponse, error) {
	if req == nil || len(scopes) == 0 || req.PageSize < 0 || req.PageSize > 1000 {
		return nil, fleeterror.NewInvalidArgumentError("invalid tank options request")
	}
	// Page size can change without invalidating the cursor; permissions cannot.
	key, err := fingerprint(orgID, &pb.ListTankOptionsRequest{}, scopes)
	if err != nil {
		return nil, err
	}
	cursor, err := decodeCursor(req.PageToken, key)
	if err != nil {
		return nil, err
	}
	if req.PageToken != "" && (cursor.Kind != int32(pb.EntityKind_ENTITY_KIND_TANK) || cursor.ID <= 0) {
		return nil, fleeterror.NewInvalidArgumentError("invalid tank options cursor")
	}
	authority, err := marshalScopes(scopes)
	if err != nil {
		return nil, err
	}
	size := req.PageSize
	if size == 0 {
		size = 50
	}
	rows, err := s.store.ListFleetTankOptions(ctx, sqlc.ListFleetTankOptionsParams{
		OrgID: orgID, Authority: authority, HasCursor: req.PageToken != "", AfterName: cursor.Text, AfterID: cursor.ID, PageLimit: size + 1,
	})
	if err != nil {
		return nil, err
	}
	out := &pb.ListTankOptionsResponse{}
	if len(rows) > int(size) {
		rows = rows[:size]
		last := rows[len(rows)-1]
		out.NextPageToken, err = encodeCursor(pageCursor{Fingerprint: key, Kind: int32(pb.EntityKind_ENTITY_KIND_TANK), ID: last.ID, Text: last.Name})
		if err != nil {
			return nil, err
		}
	}
	for _, row := range rows {
		out.Options = append(out.Options, &pb.TankOption{Id: row.ID, Name: row.Name, SiteName: row.SiteName})
	}
	return out, nil
}
