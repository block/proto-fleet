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

func (s *SQLPoolConfigurationStore) GetPoolConfiguration(ctx context.Context, poolConfigurationID int64) (*pb.PoolConfiguration, error) {
	poolConfiguration, err := s.GetQueries(ctx).GetPoolConfiguration(ctx, poolConfigurationID)
	if err != nil {
		return nil, err
	}

	return convertToProtoPoolConfiguration(poolConfiguration), nil
}

func (s *SQLPoolConfigurationStore) ListPoolConfigurations(ctx context.Context, orgID int64) ([]*pb.PoolConfiguration, error) {
	poolConfigurations, err := s.GetQueries(ctx).ListPoolConfigurations(ctx, orgID)
	if err != nil {
		return nil, err
	}

	result := make([]*pb.PoolConfiguration, len(poolConfigurations))
	for i, poolConfiguration := range poolConfigurations {
		result[i] = convertToProtoPoolConfiguration(poolConfiguration)
	}

	return result, nil
}

func (s *SQLPoolConfigurationStore) CreatePoolConfiguration(ctx context.Context, config *pb.PoolConfigurationConfig, orgID int64) (int64, error) {
	result, err := s.GetQueries(ctx).CreatePoolConfiguration(ctx, sqlc.CreatePoolConfigurationParams{
		OrgID:       orgID,
		Name:        config.Name,
		Description: sql.NullString{String: config.Description, Valid: true},
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("error creating pool configuration: %v", err)
	}

	poolConfigurationID, err := result.LastInsertId()
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("error getting pool configuration id: %v", err)
	}

	return poolConfigurationID, nil
}

func (s *SQLPoolConfigurationStore) DeletePoolConfiguration(ctx context.Context, poolConfigurationID int64) error {
	return s.GetQueries(ctx).DeletePoolConfiguration(ctx, poolConfigurationID)
}

func (s *SQLPoolConfigurationStore) AddPoolToConfiguration(ctx context.Context, poolConfigurationID int64, poolID int64, priority int32) (int64, error) {
	result, err := s.GetQueries(ctx).AddPoolToConfiguration(ctx, sqlc.AddPoolToConfigurationParams{PoolConfigurationID: poolConfigurationID, PoolID: poolID, Priority: priority})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("error creating pool to configuration relation: %v", err)
	}

	poolToConfigurationID, err := result.LastInsertId()
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("error getting relation id: %v", err)
	}

	return poolToConfigurationID, nil
}

func (s *SQLPoolConfigurationStore) RemovePoolFromConfiguration(ctx context.Context, poolConfigurationPoolID int64) error {
	return s.GetQueries(ctx).RemovePoolFromConfiguration(ctx, poolConfigurationPoolID)
}

func (s *SQLPoolConfigurationStore) GetPoolConfigurationsWithPools(ctx context.Context, orgID int64) ([]*pb.PoolConfigurationWithPools, error) {
	rows, err := s.GetQueries(ctx).GetPoolConfigurationsWithPools(ctx, orgID)
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
				PoolConfiguration: &pb.PoolConfiguration{
					PoolConfigurationId: row.PoolConfigID,
					Name:                row.PoolConfigName,
					Description:         description,
				},
				Pools: []*pb.PoolConfigurationPoolWithPriority{},
			}
			configMap[row.PoolConfigID] = config
		}

		// Add pool if it exists (LEFT JOIN might return null pools)
		if row.PoolID.Valid {
			isDefault := false
			if row.PoolIsDefault.Valid {
				isDefault = row.PoolIsDefault.Bool
			}

			pool := &pb.PoolConfigurationPoolWithPriority{
				Pool: &pb.Pool{
					PoolId:    row.PoolID.Int64,
					PoolName:  row.PoolName.String,
					Url:       row.PoolUrl.String,
					Username:  row.PoolUsername.String,
					IsDefault: isDefault,
				},
				Priority:                row.PoolPriority.Int32,
				PoolConfigurationPoolId: row.PoolConfigPoolID.Int64,
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

func (s *SQLPoolConfigurationStore) GetPoolConfigurationPoolWithPriority(ctx context.Context, poolConfigurationPoolID int64) (*pb.PoolConfigurationPoolWithPriority, error) {
	row, err := s.GetQueries(ctx).GetPoolConfigurationPoolWithPriority(ctx, poolConfigurationPoolID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting pool configuration pool with priority: %v", err)
	}

	isDefault := false
	if row.PoolIsDefault.Valid {
		isDefault = row.PoolIsDefault.Bool
	}

	return &pb.PoolConfigurationPoolWithPriority{
		Pool: &pb.Pool{
			PoolId:    row.PoolID,
			PoolName:  row.PoolName,
			Url:       row.PoolUrl,
			Username:  row.PoolUsername,
			IsDefault: isDefault,
		},
		Priority:                row.PoolPriority,
		PoolConfigurationPoolId: row.PoolConfigPoolID,
	}, nil
}

func convertToProtoPoolConfiguration(poolConfiguration sqlc.PoolConfiguration) *pb.PoolConfiguration {
	description := ""
	if poolConfiguration.Description.Valid {
		description = poolConfiguration.Description.String
	}

	return &pb.PoolConfiguration{
		PoolConfigurationId: poolConfiguration.ID,
		Name:                poolConfiguration.Name,
		Description:         description,
	}
}
