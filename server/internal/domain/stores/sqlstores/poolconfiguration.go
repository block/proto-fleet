package sqlstores

import (
	"context"
	"database/sql"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.PoolConfigurationStore = &SQLPoolConfigurationStore{}

type SQLPoolConfigurationStore struct {
	SQLConnectionManager
}

func NewSQLPoolConfigurationStore(conn *sql.DB) *SQLPoolConfigurationStore {
	return &SQLPoolConfigurationStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLPoolConfigurationStore) ListPoolConfigurations(ctx context.Context, orgID int64) ([]*pb.PoolConfigurationWithPools, error) {
	rows, err := s.GetQueries(ctx).ListPoolConfigurations(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting pool configuration with pools: %v", err)
	}

	configMap := make(map[int64]*pb.PoolConfigurationWithPools)

	for _, row := range rows {
		config, exists := configMap[row.PoolConfigID]
		if !exists {
			description := ""
			if row.PoolConfigDescription.Valid {
				description = row.PoolConfigDescription.String
			}

			config = &pb.PoolConfigurationWithPools{
				Configuration: &pb.PoolConfiguration{
					Id:          row.PoolConfigID,
					Name:        row.PoolConfigName,
					Description: description,
				},
				Pools: []*pb.PoolWithPriority{},
			}
			configMap[row.PoolConfigID] = config
		}

		// Add pool if it exists (LEFT JOIN might return null pools)
		if row.PoolID.Valid {
			isDefault := false
			if row.PoolIsDefault.Valid {
				isDefault = row.PoolIsDefault.Bool
			}

			pool := &pb.PoolWithPriority{
				Pool: &pb.Pool{
					PoolId:    row.PoolID.Int64,
					PoolName:  row.PoolName.String,
					Url:       row.PoolUrl.String,
					Username:  row.PoolUsername.String,
					IsDefault: isDefault,
				},
				Priority: row.PoolPriority.Int32,
			}

			config.Pools = append(config.Pools, pool)
		}
	}

	result := make([]*pb.PoolConfigurationWithPools, 0, len(configMap))
	for _, config := range configMap {
		result = append(result, config)
	}

	return result, nil
}

func (s *SQLPoolConfigurationStore) GetPoolConfiguration(ctx context.Context, orgID int64, configurationID int64) (*pb.PoolConfigurationWithPools, error) {
	rows, err := s.GetQueries(ctx).GetPoolConfiguration(ctx, sqlc.GetPoolConfigurationParams{OrgID: orgID, ID: configurationID})
	if err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		return nil, fleeterror.NewInternalErrorf("pool configuration not found")
	}

	var config *pb.PoolConfigurationWithPools

	firstRow := rows[0]
	description := ""
	if firstRow.PoolConfigDescription.Valid {
		description = firstRow.PoolConfigDescription.String
	}

	config = &pb.PoolConfigurationWithPools{
		Configuration: &pb.PoolConfiguration{
			Id:          firstRow.PoolConfigID,
			Name:        firstRow.PoolConfigName,
			Description: description,
		},
		Pools: []*pb.PoolWithPriority{},
	}

	for _, row := range rows {
		// Add pool if it exists (LEFT JOIN might return null pools)
		if row.PoolID.Valid {
			isDefault := false
			if row.PoolIsDefault.Valid {
				isDefault = row.PoolIsDefault.Bool
			}

			pool := &pb.PoolWithPriority{
				Pool: &pb.Pool{
					PoolId:    row.PoolID.Int64,
					PoolName:  row.PoolName.String,
					Url:       row.PoolUrl.String,
					Username:  row.PoolUsername.String,
					IsDefault: isDefault,
				},
				Priority: row.PoolPriority.Int32,
			}

			config.Pools = append(config.Pools, pool)
		}
	}

	return config, nil
}

func (s *SQLPoolConfigurationStore) GetPoolConfigurationIDByOrg(ctx context.Context, orgID int64) (int64, error) {
	return s.GetQueries(ctx).GetPoolConfigurationIDByOrg(ctx, orgID)
}

func (s *SQLPoolConfigurationStore) DeletePoolConfiguration(ctx context.Context, orgID int64, configurationID int64) error {
	return s.GetQueries(ctx).DeletePoolConfiguration(ctx, sqlc.DeletePoolConfigurationParams{OrgID: orgID, ID: configurationID})
}

func (s *SQLPoolConfigurationStore) DeletePoolConfigurationPools(ctx context.Context, configID int64) error {
	return s.GetQueries(ctx).DeletePoolConfigurationPools(ctx, configID)
}

func (s *SQLPoolConfigurationStore) AddPoolToConfiguration(ctx context.Context, poolConfigurationID int64, poolID int64, priority int32) error {
	return s.GetQueries(ctx).AddPoolToConfiguration(ctx, sqlc.AddPoolToConfigurationParams{PoolConfigurationID: poolConfigurationID, PoolID: poolID, Priority: priority})
}

func (s *SQLPoolConfigurationStore) UpsertPoolConfiguration(ctx context.Context, orgID int64, config *pb.PoolConfigurationBase) error {
	return s.GetQueries(ctx).UpsertPoolConfiguration(ctx, sqlc.UpsertPoolConfigurationParams{
		OrgID:       orgID,
		Name:        config.Name,
		Description: sql.NullString{String: config.Description, Valid: len(config.Description) > 0},
	})
}
