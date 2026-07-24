package sqlstores

import (
	"context"
	"database/sql"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/chat"
)

type SQLLLMConfigStore struct {
	SQLConnectionManager
}

func NewSQLLLMConfigStore(conn *sql.DB) *SQLLLMConfigStore {
	return &SQLLLMConfigStore{SQLConnectionManager: NewSQLConnectionManager(conn)}
}

var _ chat.ConfigStore = (*SQLLLMConfigStore)(nil)

func llmConfigToRecord(row sqlc.LlmConfig) chat.ConfigRecord {
	return chat.ConfigRecord{
		OrganizationID:       row.OrganizationID,
		Harness:              chat.Harness(row.Harness),
		Provider:             chat.Provider(row.Provider),
		APIKeyEncrypted:      row.ApiKeyEncrypted,
		BaseURL:              row.BaseUrl,
		Model:                row.Model,
		Temperature:          row.Temperature,
		GooseBaseURL:         row.GooseBaseUrl,
		GooseSecretEncrypted: row.GooseSecretEncrypted,
	}
}

func (s *SQLLLMConfigStore) Get(ctx context.Context, orgID int64) (chat.ConfigRecord, error) {
	row, err := s.GetQueries(ctx).GetLLMConfig(ctx, orgID)
	if err != nil {
		return chat.ConfigRecord{}, err
	}
	return llmConfigToRecord(row), nil
}

func (s *SQLLLMConfigStore) Upsert(ctx context.Context, record chat.ConfigRecord) (chat.ConfigRecord, error) {
	row, err := s.GetQueries(ctx).UpsertLLMConfig(ctx, sqlc.UpsertLLMConfigParams{
		OrganizationID:       record.OrganizationID,
		Harness:              string(record.Harness),
		Provider:             string(record.Provider),
		ApiKeyEncrypted:      record.APIKeyEncrypted,
		BaseUrl:              record.BaseURL,
		Model:                record.Model,
		Temperature:          record.Temperature,
		GooseBaseUrl:         record.GooseBaseURL,
		GooseSecretEncrypted: record.GooseSecretEncrypted,
	})
	if err != nil {
		return chat.ConfigRecord{}, err
	}
	return llmConfigToRecord(row), nil
}
