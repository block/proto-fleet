package poolconfigurations

import (
	"context"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/pooldial"
	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
)

type ConnectionTester interface {
	ValidateConnection(ctx context.Context, url, username string, password *secrets.Text, timeout *time.Duration) (bool, error)
}

func (s *Service) WithConnectionTester(tester ConnectionTester) *Service {
	s.connectionTester = tester
	return s
}

// TestConnection tests one draft slot using the editor's preview worker name.
// Saved credentials can only accompany the same destination and payout user.
// The network probe runs once, outside the retryable authorization transaction;
// it neither saves the draft nor establishes an equipment application result.
func (s *Service) TestConnection(ctx context.Context, accountID, configID string, version int64, slot SlotOptions) (bool, error) {
	password, err := withAuthority(ctx, s, accountID, Manage, func(q sqlc.Querier, p Principal) (*secrets.Text, error) {
		if _, err := Validate(RevisionOptions{Name: "Connection test", Slots: []SlotOptions{slot}}); err != nil {
			return nil, err
		}
		if (configID == "" && (version != 0 || slot.ID != "")) || (configID != "" && version <= 0) {
			return nil, fleeterror.NewInvalidArgumentError("saved pool tests require the configuration version")
		}
		var previous []StoredSlot
		if configID != "" {
			config, err := q.LockPoolConfiguration(ctx, sqlc.LockPoolConfigurationParams{OrganizationID: p.Scope.OrganizationID, AccountID: p.Scope.account(), ConfigurationID: configID})
			if err != nil {
				return nil, storageError(err)
			}
			if config.Archived || config.Version != version {
				return nil, fleeterror.NewFailedPreconditionError("configuration changed; refresh before testing")
			}
			revision, err := q.GetPoolConfigurationRevision(ctx, sqlc.GetPoolConfigurationRevisionParams{OrganizationID: p.Scope.OrganizationID, AccountID: p.Scope.account(), ConfigurationID: configID, RevisionID: config.CurrentRevisionID.String})
			if err != nil {
				return nil, storageError(err)
			}
			previous, err = DecodeSlots(revision.Slots)
			if err != nil {
				return nil, err
			}
		}
		var saved *StoredSlot
		for i := range previous {
			if previous[i].ID == slot.ID {
				saved = &previous[i]
				break
			}
		}
		if slot.ID != "" && saved == nil {
			return nil, fleeterror.NewInvalidArgumentError("saved pool slot not found")
		}
		if slot.Password != nil {
			return secrets.NewText(*slot.Password), nil
		}
		if saved == nil {
			return secrets.NewText(""), nil
		}
		if saved.URL != slot.URL || saved.Username != slot.Username {
			return nil, fleeterror.NewInvalidArgumentError("changed pool destination or username requires an explicit password; use empty to clear")
		}
		plain, err := s.repository.secrets.Decrypt(saved.PasswordCiphertext)
		if err != nil {
			return nil, fleeterror.NewInternalError("read pool credential failed")
		}
		return secrets.NewText(string(plain)), nil
	})
	if err != nil {
		return false, err
	}
	username, err := RenderWorker(slot.Username, slot.WorkerTemplate, "123", "Miner-123")
	if err != nil {
		return false, err
	}
	if s.connectionTester == nil {
		return false, fleeterror.NewFailedPreconditionError("pool connection testing is unavailable")
	}
	timeout := 5 * time.Second
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if accountID != "" {
		probeCtx = pooldial.PublicOnly(probeCtx)
	}
	connected, err := s.connectionTester.ValidateConnection(probeCtx, slot.URL, username, password, &timeout)
	// Transport errors can include endpoints, usernames or authentication data.
	// They are intentionally neither returned nor logged.
	return connected && err == nil, nil
}
