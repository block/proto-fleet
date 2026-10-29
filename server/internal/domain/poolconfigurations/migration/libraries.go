// Package migration collects legacy operator and hosted pool libraries for the
// offline configuration import. Runtime configuration storage is account-neutral.
package migration

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
)

// Libraries must run in a transaction while application writers are stopped.
// It preserves old records and does not establish equipment assignments. The
// returned source mappings support the later assignment adoption and cutover.
func Libraries(ctx context.Context, q sqlc.Querier, secrets *encrypt.Service, orgID int64) (poolconfigurations.ImportReport, error) {
	if orgID <= 0 {
		return poolconfigurations.ImportReport{}, fleeterror.NewInvalidArgumentError("invalid import organization")
	}
	if _, err := q.LockPoolConfigurationImportOrganization(ctx, orgID); err != nil {
		return poolconfigurations.ImportReport{}, fleeterror.NewInternalError("lock pool import organization failed")
	}
	if err := q.LockLegacyPoolLibraryForImport(ctx); err != nil {
		return poolconfigurations.ImportReport{}, fleeterror.NewInternalError("lock legacy pool library failed")
	}
	legacy, err := q.ListPools(ctx, orgID)
	if err != nil {
		return poolconfigurations.ImportReport{}, fleeterror.NewInternalError("read legacy pool library failed")
	}
	candidates := make([]poolconfigurations.ImportCandidate, 0, len(legacy))
	for _, pool := range legacy {
		ciphertext := pool.PasswordEnc
		if ciphertext == "" {
			// The legacy operator store uses an empty ciphertext for an omitted
			// password. That is known empty, unlike missing device readback.
			ciphertext, err = secrets.Encrypt(nil)
			if err != nil {
				return poolconfigurations.ImportReport{}, fleeterror.NewInternalError("encrypt imported empty pool password failed")
			}
		}
		worker := ""
		username := strings.TrimSpace(pool.Username)
		if username != "" && !strings.Contains(username, ".") {
			worker = "{miner_name}"
		}
		name := strings.TrimSpace(pool.PoolName)
		if name == "" {
			name = "Imported pool " + strconv.FormatInt(pool.ID, 10)
		}
		candidates = append(candidates, poolconfigurations.ImportCandidate{
			Scope:  poolconfigurations.Scope{OrganizationID: orgID},
			Source: poolconfigurations.ImportSource{Kind: "operator_pool", ID: strconv.FormatInt(pool.ID, 10), Version: pool.UpdatedAt.UTC().Format(time.RFC3339Nano)},
			Name:   name, Slots: []poolconfigurations.StoredSlot{{URL: pool.Url, Username: pool.Username, WorkerTemplate: worker, PasswordCiphertext: ciphertext}},
		})
	}
	hosted, err := q.ListHostedPoolImportSources(ctx, orgID)
	if err != nil {
		return poolconfigurations.ImportReport{}, fleeterror.NewInternalError("read hosted pool library failed")
	}
	for _, revision := range hosted {
		slots, err := poolconfigurations.DecodeSlots(revision.Slots)
		if err != nil {
			return poolconfigurations.ImportReport{}, err
		}
		candidates = append(candidates, poolconfigurations.ImportCandidate{
			Scope:  poolconfigurations.Scope{OrganizationID: orgID, AccountID: revision.OwnerAccountID},
			Source: poolconfigurations.ImportSource{Kind: "hosted_revision", ID: revision.RevisionID, Version: strconv.FormatInt(revision.Number, 10)},
			Name:   revision.Name, Slots: slots,
		})
	}
	return poolconfigurations.NewRepository(secrets).Import(ctx, q, orgID, candidates)
}
