package hostedaccounts

import (
	"context"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// HostGroupCredentials is private dispatch material, never an API response.
// Callers must authorize the container and profile before calling it.
// Retained for pre-cutover reconciliation and migration, including archived
// revisions. New assignments use canonical configuration execution snapshots.
func (s *PoolService) HostGroupCredentials(ctx context.Context, q sqlc.Querier, orgID int64, revisionID string) ([]sdk.MiningGroupPoolInput, error) {
	r, err := q.GetHostMiningGroupPoolRevisionForReconciliation(ctx, sqlc.GetHostMiningGroupPoolRevisionForReconciliationParams{OrganizationID: orgID, RevisionID: revisionID})
	if err != nil {
		return nil, err
	}
	slots, err := poolconfigurations.DecodeSlots(r.Slots)
	if err != nil {
		return nil, err
	}
	out := make([]sdk.MiningGroupPoolInput, 0, len(slots))
	for _, slot := range slots {
		if slot.WorkerTemplate != "" {
			return nil, fleeterror.NewFailedPreconditionError("mining groups require fixed worker usernames")
		}
		password, err := s.secrets.Decrypt(slot.PasswordCiphertext)
		if err != nil {
			return nil, err
		}
		out = append(out, sdk.MiningGroupPoolInput{URL: slot.URL, Username: slot.Username, Password: string(password)})
	}
	rqt := sdk.MiningGroupRequest{Action: "replace_pools", GroupID: "validation", Revision: 1, Key: "validation", Pools: out}
	return out, rqt.Validate()
}
