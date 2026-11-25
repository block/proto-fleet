package poolconfigurations

import (
	"encoding/json"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestImportConsolidatesOnlyCompleteEquivalentStacksWithinOwner(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	var org, root, account int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Pools') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, org).Scan(&root))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES($1,$2,$3,1,'Customer',15) RETURNING id`, uuid.NewString(), org, root).Scan(&account))
	cipher := testCipher(t)
	repo := NewRepository(cipher)
	stack := func(password string) []StoredSlot {
		t.Helper()
		encoded, err := EncryptSlots(cipher, []SlotOptions{
			{URL: "stratum+tcp://primary.example", Username: "payout", WorkerTemplate: "{miner_name}", Password: &password},
			{URL: "stratum+tcp://backup.example", Username: "backup", Password: &password},
		}, nil)
		require.NoError(t, err)
		slots, err := DecodeSlots(encoded)
		require.NoError(t, err)
		return slots
	}
	candidates := []ImportCandidate{}
	for _, id := range []string{"a", "b", "different-owner", "different-password", "different-order", "different-worker", "unknown-password"} {
		candidates = append(candidates, ImportCandidate{Scope: Scope{OrganizationID: org}, Source: ImportSource{Kind: "hosted_revision", ID: id, Version: "1"}, Name: "Imported " + id, Slots: stack("secret")})
	}
	require.NotEqual(t, candidates[0].Slots[0].PasswordCiphertext, candidates[1].Slots[0].PasswordCiphertext)
	candidates[2].Scope.AccountID = account
	candidates[3].Slots = stack("different")
	candidates[4].Slots[0], candidates[4].Slots[1] = candidates[4].Slots[1], candidates[4].Slots[0]
	candidates[5].Slots[0].WorkerTemplate = ""
	candidates[6].Slots[0].PasswordCiphertext = ""
	run := func() ImportReport {
		t.Helper()
		report, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (ImportReport, error) {
			return repo.Import(ctx, q, org, candidates)
		})
		require.NoError(t, err)
		return report
	}
	first := run()
	require.Equal(t, 5, first.CreatedConfigurations)
	require.Len(t, first.Imported, 6)
	require.Len(t, first.Unresolved, 1)
	require.Equal(t, "unknown-password", first.Unresolved[0].Source.ID)
	bySource := map[string]ImportedConfiguration{}
	for _, imported := range first.Imported {
		bySource[imported.Source.ID] = imported
	}
	require.Equal(t, bySource["a"].ConfigurationID, bySource["b"].ConfigurationID)
	require.NotEqual(t, bySource["a"].ConfigurationID, bySource["different-owner"].ConfigurationID)
	second := run()
	require.Zero(t, second.CreatedConfigurations)
	require.Equal(t, first.Imported, second.Imported)
	var configs, queued int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration WHERE organization_id=$1`, org).Scan(&configs))
	require.Equal(t, 5, configs)
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM queue_message q JOIN device d ON d.id=q.device_id WHERE d.org_id=$1`, org).Scan(&queued))
	require.Zero(t, queued, "importing configurations never dispatches equipment changes")
	encoded, err := json.Marshal(first)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret")
	require.NotContains(t, string(encoded), "ciphertext")
	// Later source versions must preserve the same owner, password, ordering,
	// and worker distinctions when the index is seeded from persisted imports.
	for i := range candidates {
		candidates[i].Source.Version = "2"
	}
	third := run()
	require.Zero(t, third.CreatedConfigurations)
	require.Len(t, third.Imported, 6)
	for _, imported := range third.Imported {
		require.Equal(t, bySource[imported.Source.ID].ConfigurationID, imported.ConfigurationID)
		require.Equal(t, bySource[imported.Source.ID].RevisionID, imported.RevisionID)
	}
	// An immutable source identity must not be reused for different settings.
	candidates[0].Slots = stack("replaced")
	_, err = db.WithTransaction(ctx, conn, func(q sqlc.Querier) (ImportReport, error) {
		return repo.Import(ctx, q, org, candidates)
	})
	require.Error(t, err)
}

func TestImportReusesOnlyCurrentActiveImportedSettings(t *testing.T) {
	for _, scenario := range []string{"edited to different settings", "edited to matching settings", "archived", "manually created"} {
		t.Run(scenario, func(t *testing.T) {
			conn := dbtest.GetTestDB(t)
			ctx := t.Context()
			var org int64
			require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Pools') RETURNING id`, uuid.NewString()).Scan(&org))
			scope := Scope{OrganizationID: org}
			cipher := testCipher(t)
			repo := NewRepository(cipher)
			password := "original-secret"
			options := RevisionOptions{Name: "Original", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "worker", Password: &password}}}
			encoded, err := EncryptSlots(cipher, options.Slots, nil)
			require.NoError(t, err)
			slots, err := DecodeSlots(encoded)
			require.NoError(t, err)
			original := ImportCandidate{Scope: scope, Source: ImportSource{Kind: "operator_pool", ID: "legacy", Version: "1"}, Name: options.Name, Slots: slots}
			run := func(candidates ...ImportCandidate) ImportReport {
				t.Helper()
				report, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (ImportReport, error) {
					return repo.Import(ctx, q, org, candidates)
				})
				require.NoError(t, err)
				require.Empty(t, report.Unresolved)
				return report
			}
			var config Configuration
			var first ImportedConfiguration
			if scenario == "manually created" {
				config, err = db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Configuration, error) {
					return repo.Create(ctx, q, scope, options)
				})
				require.NoError(t, err)
			} else {
				first = run(original).Imported[0]
				current, err := repo.Get(ctx, sqlc.New(conn), scope, first.ConfigurationID)
				require.NoError(t, err)
				config = current.Configuration
			}
			next := original
			next.Source.Version = "2"
			next.Name = "Renamed"
			switch scenario {
			case "edited to different settings", "edited to matching settings":
				changedPassword := "edited-secret"
				options.Slots[0].Password = &changedPassword
				saved, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (SaveResult, error) {
					return repo.Revise(ctx, q, scope, config.ID, config.Version, options)
				})
				require.NoError(t, err)
				config = saved.Configuration
				if scenario == "edited to matching settings" {
					encoded, err := EncryptSlots(cipher, options.Slots, nil)
					require.NoError(t, err)
					next.Slots, err = DecodeSlots(encoded)
					require.NoError(t, err)
				}
			case "archived":
				_, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (struct{}, error) {
					return struct{}{}, repo.Archive(ctx, q, scope, config.ID, config.Version)
				})
				require.NoError(t, err)
			}
			candidates := []ImportCandidate{next}
			if scenario != "manually created" {
				// Replaying a mapped old source must not make its old revision a
				// reuse candidate for the newly discovered version.
				candidates = append(candidates, original)
			}
			report := run(candidates...)
			if scenario != "manually created" {
				require.Equal(t, first, report.Imported[0], "existing source mappings retain their original revision")
			}
			latest := report.Imported[len(report.Imported)-1]
			require.Equal(t, next.Source, latest.Source)
			if scenario == "edited to matching settings" {
				require.Zero(t, report.CreatedConfigurations)
				require.Equal(t, config.ID, latest.ConfigurationID)
				require.Equal(t, config.RevisionID, latest.RevisionID)
			} else {
				require.Equal(t, 1, report.CreatedConfigurations)
				require.NotEqual(t, config.ID, latest.ConfigurationID)
			}
		})
	}
}
