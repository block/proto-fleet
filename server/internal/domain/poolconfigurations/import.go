package poolconfigurations

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type ImportSource struct {
	Kind, ID, Version string
}

type ImportCandidate struct {
	Scope  Scope
	Source ImportSource
	Name   string
	Slots  []StoredSlot
}

type ImportedConfiguration struct {
	Source                      ImportSource
	ConfigurationID, RevisionID string
}

type ImportIssue struct {
	Source ImportSource
	Reason string
}

type ImportReport struct {
	CreatedConfigurations int
	Imported              []ImportedConfiguration
	Unresolved            []ImportIssue
}

type preparedImport struct {
	candidate ImportCandidate
	options   RevisionOptions
	key       [sha256.Size]byte
	existing  *ImportedConfiguration
}

// Import consolidates a complete organization snapshot in the caller's
// transaction. Collectors must supply authoritative owner and worker settings;
// unknown credentials are reported, never interpreted as empty passwords.
// This only writes configurations and source mappings, never device commands.
func (r *Repository) Import(ctx context.Context, q sqlc.Querier, orgID int64, candidates []ImportCandidate) (ImportReport, error) {
	var report ImportReport
	if orgID <= 0 {
		return report, fleeterror.NewInvalidArgumentError("invalid import organization")
	}
	if _, err := q.LockPoolConfigurationImportOrganization(ctx, orgID); err != nil {
		return report, storageError(err)
	}
	// A random per-run key prevents the in-memory index from becoming a reusable
	// credential fingerprint. Neither the key nor its digests are persisted.
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return report, fleeterror.NewInternalError("initialize configuration import failed")
	}
	candidates = slices.Clone(candidates)
	slices.SortFunc(candidates, func(a, b ImportCandidate) int {
		return cmp.Or(cmp.Compare(a.Source.Kind, b.Source.Kind), cmp.Compare(a.Source.ID, b.Source.ID), cmp.Compare(a.Source.Version, b.Source.Version))
	})
	seen := map[ImportSource]bool{}
	prepared := make([]preparedImport, 0, len(candidates))
	canonical := map[[sha256.Size]byte]ImportedConfiguration{}
	for _, candidate := range candidates {
		if candidate.Scope.OrganizationID != orgID || candidate.Scope.validate() != nil {
			return ImportReport{}, fleeterror.NewInvalidArgumentError("invalid import owner")
		}
		if !validImportSource(candidate.Source) || seen[candidate.Source] {
			return ImportReport{}, fleeterror.NewInvalidArgumentError("invalid or repeated import source")
		}
		seen[candidate.Source] = true
		options, reason := r.importOptions(candidate)
		if reason != "" {
			report.Unresolved = append(report.Unresolved, ImportIssue{Source: candidate.Source, Reason: reason})
			continue
		}
		fingerprint, err := importFingerprint(key[:], candidate.Scope, options.Slots)
		if err != nil {
			return ImportReport{}, err
		}
		item := preparedImport{candidate: candidate, options: options, key: fingerprint}
		existing, err := q.GetPoolConfigurationImport(ctx, sqlc.GetPoolConfigurationImportParams{
			OrganizationID: orgID, AccountID: candidate.Scope.account(), SourceKind: candidate.Source.Kind,
			SourceID: candidate.Source.ID, SourceVersion: candidate.Source.Version,
		})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ImportReport{}, storageError(err)
		}
		if err == nil {
			revision, err := q.GetPoolConfigurationRevision(ctx, sqlc.GetPoolConfigurationRevisionParams{
				OrganizationID: orgID, AccountID: candidate.Scope.account(), ConfigurationID: existing.ConfigurationID, RevisionID: existing.RevisionID,
			})
			if err != nil {
				return ImportReport{}, storageError(err)
			}
			slots, err := DecodeSlots(revision.Slots)
			if err != nil {
				return ImportReport{}, err
			}
			equal, err := r.Equivalent(slots, candidate.Slots)
			if err != nil {
				return ImportReport{}, err
			}
			if !equal {
				return ImportReport{}, fleeterror.NewFailedPreconditionError("import source changed without a new source version")
			}
			item.existing = &ImportedConfiguration{Source: candidate.Source, ConfigurationID: existing.ConfigurationID, RevisionID: existing.RevisionID}
		}
		prepared = append(prepared, item)
	}
	loadedScopes := map[Scope]bool{}
	for _, item := range prepared {
		if item.existing != nil {
			report.Imported = append(report.Imported, *item.existing)
			continue
		}
		scope := item.candidate.Scope
		if !loadedScopes[scope] {
			// Existing source mappings retain their original revisions. For new
			// sources, compare current settings so reuse cannot undo a later edit
			// or revive an archived configuration.
			rows, err := q.ListImportedPoolConfigurations(ctx, sqlc.ListImportedPoolConfigurationsParams{
				OrganizationID: orgID, AccountID: scope.account(),
			})
			if err != nil {
				return ImportReport{}, storageError(err)
			}
			for _, row := range rows {
				slots, err := DecodeSlots(row.Slots)
				if err != nil {
					return ImportReport{}, err
				}
				options, reason := r.importOptions(ImportCandidate{Name: row.Name, Slots: slots})
				if reason != "" {
					// Unreadable or incomplete settings cannot establish equality.
					continue
				}
				fingerprint, err := importFingerprint(key[:], scope, options.Slots)
				if err != nil {
					return ImportReport{}, err
				}
				if _, exists := canonical[fingerprint]; !exists {
					canonical[fingerprint] = ImportedConfiguration{ConfigurationID: row.ConfigurationID, RevisionID: row.RevisionID}
				}
			}
			loadedScopes[scope] = true
		}
		imported, exists := canonical[item.key]
		if !exists {
			config, err := r.Create(ctx, q, item.candidate.Scope, item.options)
			if err != nil {
				return ImportReport{}, err
			}
			imported = ImportedConfiguration{ConfigurationID: config.ID, RevisionID: config.RevisionID}
			canonical[item.key] = imported
			report.CreatedConfigurations++
		}
		imported.Source = item.candidate.Source
		if err := q.RecordPoolConfigurationImport(ctx, sqlc.RecordPoolConfigurationImportParams{
			OrganizationID: orgID, SourceKind: imported.Source.Kind, SourceID: imported.Source.ID, SourceVersion: imported.Source.Version,
			ConfigurationID: imported.ConfigurationID, RevisionID: imported.RevisionID,
		}); err != nil {
			return ImportReport{}, storageError(err)
		}
		report.Imported = append(report.Imported, imported)
	}
	return report, nil
}

func importFingerprint(key []byte, scope Scope, slots []SlotOptions) ([sha256.Size]byte, error) {
	payload, err := json.Marshal(struct {
		Scope Scope
		Slots []SlotOptions
	}{scope, slots})
	if err != nil {
		return [sha256.Size]byte{}, fleeterror.NewInternalError("encode configuration import failed")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	return [sha256.Size]byte(mac.Sum(nil)), nil
}

func validImportSource(source ImportSource) bool {
	if source.ID == "" || source.Version == "" {
		return false
	}
	switch source.Kind {
	case "operator_pool", "hosted_revision", "device_readback":
		return true
	default:
		return false
	}
}

func (r *Repository) importOptions(candidate ImportCandidate) (RevisionOptions, string) {
	options := RevisionOptions{Name: candidate.Name, Slots: make([]SlotOptions, len(candidate.Slots))}
	for i, slot := range candidate.Slots {
		if slot.PasswordCiphertext == "" {
			return RevisionOptions{}, "Pool password is unknown; supply the complete configuration before adoption."
		}
		plain, err := r.secrets.Decrypt(slot.PasswordCiphertext)
		if err != nil {
			return RevisionOptions{}, "Saved pool credentials could not be read."
		}
		password := string(plain)
		options.Slots[i] = SlotOptions{URL: slot.URL, Username: slot.Username, WorkerTemplate: slot.WorkerTemplate, Password: &password}
	}
	validated, err := Validate(options)
	if err != nil {
		return RevisionOptions{}, "Pool configuration requires review before adoption."
	}
	return validated, ""
}
