package hostedaccounts

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/sv2"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
)

type PoolSlotOptions struct {
	URL, Username, WorkerTemplate string
	// Nil retains the previous password at this priority when revising; a new
	// slot gets an encrypted empty password. Explicit empty clears it.
	Password *string
}

type PoolRevisionOptions struct {
	Name  string
	Slots []PoolSlotOptions
}

// PoolEndpoint deliberately has no plaintext or encrypted credential field.
type PoolEndpoint struct {
	Priority                      int32
	URL, Username, WorkerTemplate string
	HasPassword                   bool
}

type PoolRevision struct {
	PoolID, RevisionID, Name string
	Number                   int64
	CreatedAt                time.Time
	Slots                    []PoolEndpoint
	Archived                 bool
}

type PoolListPage struct {
	Configurations []PoolRevision
	NextPageToken  string
}

type PoolProfile struct {
	PoolID     string
	RevisionID string
	Revision   int64
	UpdatedAt  time.Time
}

type CredentialsVerifier interface {
	VerifySessionCredentials(ctx context.Context, username, password string) error
}

type PoolService struct {
	accounts *Service
	secrets  *encrypt.Service
	keys     *KeyService
	verifier CredentialsVerifier
}

func NewPoolService(accounts *Service, secrets *encrypt.Service, keys *KeyService, verifier CredentialsVerifier) *PoolService {
	return &PoolService{accounts: accounts, secrets: secrets, keys: keys, verifier: verifier}
}

type storedPoolSlot struct {
	URL                string `json:"url"`
	Username           string `json:"username"`
	WorkerTemplate     string `json:"worker_template"`
	PasswordCiphertext string `json:"password_ciphertext"`
	HasPassword        bool   `json:"has_password"`
}

func (s *PoolService) Create(ctx context.Context, actorID, orgID int64, accountID string, options PoolRevisionOptions) (PoolRevision, error) {
	options, err := validatePoolOptions(options)
	if err != nil {
		return PoolRevision{}, err
	}
	return db.WithTransaction(ctx, s.accounts.conn, func(q sqlc.Querier) (PoolRevision, error) {
		actor, account, err := s.authorizeWrite(ctx, q, actorID, orgID, accountID, ManagePools)
		if err != nil {
			return PoolRevision{}, err
		}
		slots, err := s.encryptSlots(options.Slots, nil)
		if err != nil {
			return PoolRevision{}, err
		}
		pool, err := q.CreateHostedAccountPool(ctx, sqlc.CreateHostedAccountPoolParams{PoolID: uuid.NewString(), OrganizationID: orgID, AccountID: account.ID})
		if err != nil {
			return PoolRevision{}, poolStorageError(err)
		}
		revision, err := q.CreateHostedAccountPoolRevision(ctx, sqlc.CreateHostedAccountPoolRevisionParams{RevisionID: uuid.NewString(), OrganizationID: orgID, AccountID: account.ID, PoolID: pool.PoolID, Number: 1, Name: options.Name, Slots: slots})
		if err != nil {
			return PoolRevision{}, poolStorageError(err)
		}
		if err := audit(ctx, q, actor, account, "create_hosted_account_pool", map[string]any{"pool_id": pool.PoolID, "revision_id": revision.RevisionID, "revision": revision.Number, "slot_count": len(options.Slots)}); err != nil {
			return PoolRevision{}, err
		}
		return publicPoolRevision(revision, pool.Archived)
	})
}

func (s *PoolService) Revise(ctx context.Context, actorID, orgID int64, accountID, poolID, expectedRevisionID string, options PoolRevisionOptions) (PoolRevision, error) {
	options, err := validatePoolOptions(options)
	if err != nil {
		return PoolRevision{}, err
	}
	if !validPoolID(poolID) || !validPoolID(expectedRevisionID) {
		return PoolRevision{}, fleeterror.NewInvalidArgumentError("pool and expected revision identifiers are required")
	}
	return db.WithTransaction(ctx, s.accounts.conn, func(q sqlc.Querier) (PoolRevision, error) {
		actor, account, err := s.authorizeWrite(ctx, q, actorID, orgID, accountID, ManagePools)
		if err != nil {
			return PoolRevision{}, err
		}
		pool, err := q.GetHostedAccountPool(ctx, sqlc.GetHostedAccountPoolParams{OrganizationID: orgID, AccountID: account.ID, PoolID: poolID})
		if err != nil {
			return PoolRevision{}, poolStorageError(err)
		}
		if pool.Archived {
			return PoolRevision{}, fleeterror.NewFailedPreconditionError("archived configurations cannot be revised")
		}
		previous, err := q.GetHostedAccountPoolRevision(ctx, sqlc.GetHostedAccountPoolRevisionParams{OrganizationID: orgID, AccountID: account.ID, PoolID: poolID})
		if err != nil {
			return PoolRevision{}, poolStorageError(err)
		}
		if previous.RevisionID != expectedRevisionID {
			return PoolRevision{}, fleeterror.NewFailedPreconditionError("pool configuration has a newer revision")
		}
		oldSlots, err := decodePoolSlots(previous.Slots)
		if err != nil {
			return PoolRevision{}, err
		}
		slots, err := s.encryptSlots(options.Slots, oldSlots)
		if err != nil {
			return PoolRevision{}, err
		}
		revision, err := q.CreateHostedAccountPoolRevision(ctx, sqlc.CreateHostedAccountPoolRevisionParams{RevisionID: uuid.NewString(), OrganizationID: orgID, AccountID: account.ID, PoolID: poolID, Number: previous.Number + 1, Name: options.Name, Slots: slots})
		if err != nil {
			return PoolRevision{}, poolStorageError(err)
		}
		if err := audit(ctx, q, actor, account, "revise_hosted_account_pool", map[string]any{"pool_id": poolID, "previous_revision_id": previous.RevisionID, "revision_id": revision.RevisionID, "revision": revision.Number, "slot_count": len(options.Slots)}); err != nil {
			return PoolRevision{}, err
		}
		return publicPoolRevision(revision, false)
	})
}

func (s *PoolService) Get(ctx context.Context, actorID, orgID int64, accountID, poolID, revisionID string) (PoolRevision, error) {
	if !validPoolID(poolID) || (revisionID != "" && !validPoolID(revisionID)) {
		return PoolRevision{}, fleeterror.NewInvalidArgumentError("invalid pool identifier")
	}
	account, err := s.authorizeRead(ctx, actorID, orgID, accountID)
	if err != nil {
		return PoolRevision{}, err
	}
	pool, err := s.accounts.queries.GetHostedAccountPool(ctx, sqlc.GetHostedAccountPoolParams{OrganizationID: orgID, AccountID: account.ID, PoolID: poolID})
	if err != nil {
		return PoolRevision{}, poolStorageError(err)
	}
	revision, err := s.accounts.queries.GetHostedAccountPoolRevision(ctx, sqlc.GetHostedAccountPoolRevisionParams{OrganizationID: orgID, AccountID: account.ID, PoolID: poolID, RevisionID: revisionID})
	if err != nil {
		return PoolRevision{}, poolStorageError(err)
	}
	return publicPoolRevision(revision, pool.Archived)
}

type poolCursor struct {
	OrganizationID  int64  `json:"org"`
	AccountID       string `json:"account"`
	IncludeArchived bool   `json:"archived"`
	AfterPoolID     string `json:"after"`
}

func (s *PoolService) List(ctx context.Context, actorID, orgID int64, accountID, pageToken string, pageSize int32, includeArchived bool) (PoolListPage, error) {
	if pageSize == 0 {
		pageSize = 50
	}
	if pageSize < 1 || pageSize > 100 {
		return PoolListPage{}, fleeterror.NewInvalidArgumentError("page size must be between 1 and 100")
	}
	account, err := s.authorizeRead(ctx, actorID, orgID, accountID)
	if err != nil {
		return PoolListPage{}, err
	}
	var after string
	if pageToken != "" {
		var cursor poolCursor
		encoded, err := base64.RawURLEncoding.DecodeString(pageToken)
		if err != nil || json.Unmarshal(encoded, &cursor) != nil || cursor.OrganizationID != orgID || cursor.AccountID != account.AccountID || cursor.IncludeArchived != includeArchived || !validPoolID(cursor.AfterPoolID) {
			return PoolListPage{}, fleeterror.NewInvalidArgumentError("invalid pool page token")
		}
		after = cursor.AfterPoolID
	}
	rows, err := s.accounts.queries.ListHostedAccountPools(ctx, sqlc.ListHostedAccountPoolsParams{OrganizationID: orgID, AccountID: account.ID, IncludeArchived: includeArchived, AfterPoolID: after, PageLimit: pageSize + 1})
	if err != nil {
		return PoolListPage{}, poolStorageError(err)
	}
	page := PoolListPage{Configurations: make([]PoolRevision, 0, min(len(rows), int(pageSize)))}
	for i, row := range rows {
		if i == int(pageSize) {
			break
		}
		revision, err := publicPoolRevision(sqlc.HostedAccountPoolRevision{RevisionID: row.RevisionID, OrganizationID: row.OrganizationID, AccountID: row.AccountID, PoolID: row.PoolID, Number: row.Number, Name: row.Name, Slots: row.Slots, CreatedAt: row.CreatedAt}, row.Archived)
		if err != nil {
			return PoolListPage{}, err
		}
		page.Configurations = append(page.Configurations, revision)
	}
	if len(rows) > int(pageSize) {
		encoded, err := json.Marshal(poolCursor{orgID, account.AccountID, includeArchived, page.Configurations[len(page.Configurations)-1].PoolID})
		if err != nil {
			return PoolListPage{}, fleeterror.NewInternalError("encode pool page token failed")
		}
		page.NextPageToken = base64.RawURLEncoding.EncodeToString(encoded)
	}
	return page, nil
}

func (s *PoolService) Archive(ctx context.Context, actorID, orgID int64, accountID, poolID string) error {
	if !validPoolID(poolID) {
		return fleeterror.NewInvalidArgumentError("invalid pool identifier")
	}
	return db.WithTransactionNoResult(ctx, s.accounts.conn, func(q sqlc.Querier) error {
		actor, account, err := s.authorizeWrite(ctx, q, actorID, orgID, accountID, ManagePools)
		if err != nil {
			return err
		}
		if _, err := q.GetHostedAccountPool(ctx, sqlc.GetHostedAccountPoolParams{OrganizationID: orgID, AccountID: account.ID, PoolID: poolID}); err != nil {
			return poolStorageError(err)
		}
		n, err := q.ArchiveHostedAccountPool(ctx, sqlc.ArchiveHostedAccountPoolParams{OrganizationID: orgID, AccountID: account.ID, PoolID: poolID})
		if err != nil {
			return poolStorageError(err)
		}
		if n != 1 {
			return fleeterror.NewFailedPreconditionError("change or clear the incoming profile before archiving its configuration")
		}
		return audit(ctx, q, actor, account, "archive_hosted_account_pool", map[string]any{"pool_id": poolID})
	})
}

func (s *PoolService) IncomingProfile(ctx context.Context, actorID, orgID int64, accountID string) (PoolProfile, error) {
	account, err := s.authorizeRead(ctx, actorID, orgID, accountID)
	if err != nil {
		return PoolProfile{}, err
	}
	row, err := s.accounts.queries.GetHostedAccountPoolProfile(ctx, sqlc.GetHostedAccountPoolProfileParams{OrganizationID: orgID, AccountID: account.ID})
	if errors.Is(err, sql.ErrNoRows) {
		return PoolProfile{}, nil
	}
	if err != nil {
		return PoolProfile{}, poolStorageError(err)
	}
	return PoolProfile{PoolID: row.PoolID.String, RevisionID: row.RevisionID.String, Revision: row.Revision, UpdatedAt: row.UpdatedAt}, nil
}

// SetIncomingProfile selects a pinned revision for future incoming allocations.
// No current miner configuration changes here. Live authority admission precedes
// password verification without locks; the transaction rechecks every bound.
func (s *PoolService) SetIncomingProfile(ctx context.Context, actorID, orgID int64, accountID, revisionID, password string) error {
	if revisionID != "" && !validPoolID(revisionID) {
		return fleeterror.NewInvalidArgumentError("invalid pool revision identifier")
	}
	info, err := session.GetInfo(ctx)
	if err != nil || info.UserID != actorID || info.OrganizationID != orgID {
		return denied()
	}
	// Reject callers without live pool-change authority before loading credentials
	// or spending the issuer's shared password-verification allowance.
	if _, err := s.authorizePoolAccess(ctx, s.accounts.queries, actorID, orgID, accountID, ChangePools); err != nil {
		return err
	}
	if s.verifier == nil {
		return fleeterror.NewInternalError("credential verification is unavailable")
	}
	// Bind the proof to the credential observed before verification. A reset
	// during verification or lock acquisition invalidates it; hashing remains
	// outside the organization lock. The locked actor prevents a later reset
	// from racing the actual profile mutation.
	credential, err := s.accounts.queries.GetUserById(ctx, actorID)
	if err != nil {
		return denied()
	}
	if err := s.verifier.VerifySessionCredentials(ctx, info.Username, password); err != nil {
		return err
	}
	return db.WithTransactionNoResult(ctx, s.accounts.conn, func(q sqlc.Querier) error {
		actor, account, err := s.authorizeWrite(ctx, q, actorID, orgID, accountID, ChangePools)
		if err != nil {
			return err
		}
		if actor.PasswordHash != credential.PasswordHash || actor.Username != credential.Username || actor.PasswordUpdatedAt != credential.PasswordUpdatedAt {
			return fleeterror.NewForbiddenError("credentials changed; verify again")
		}
		if revisionID != "" {
			revision, err := q.GetHostedAccountPoolRevisionByID(ctx, sqlc.GetHostedAccountPoolRevisionByIDParams{OrganizationID: orgID, AccountID: account.ID, RevisionID: revisionID})
			if err != nil {
				return poolStorageError(err)
			}
			pool, err := q.GetHostedAccountPool(ctx, sqlc.GetHostedAccountPoolParams{OrganizationID: orgID, AccountID: account.ID, PoolID: revision.PoolID})
			if err != nil {
				return poolStorageError(err)
			}
			if pool.Archived {
				return fleeterror.NewFailedPreconditionError("archived configurations cannot be selected")
			}
		}
		profileRevision, err := q.SetHostedAccountPoolProfile(ctx, sqlc.SetHostedAccountPoolProfileParams{OrganizationID: orgID, AccountID: account.ID, RevisionID: sql.NullString{String: revisionID, Valid: revisionID != ""}})
		if err != nil {
			return poolStorageError(err)
		}
		return audit(ctx, q, actor, account, "set_hosted_account_pool_profile", map[string]any{"revision_id": revisionID, "profile_revision": profileRevision})
	})
}

func (s *PoolService) authorizeRead(ctx context.Context, actorID, orgID int64, accountID string) (Account, error) {
	path, err := loadPath(ctx, s.accounts.queries, orgID, accountID)
	if err != nil {
		return Account{}, err
	}
	if _, err := authorize(ctx, s.accounts.queries, actorID, orgID, path, View); err != nil {
		return Account{}, err
	}
	return path[len(path)-1], nil
}

func (s *PoolService) authorizeWrite(ctx context.Context, q sqlc.Querier, actorID, orgID int64, accountID string, required Capabilities) (sqlc.User, Account, error) {
	actor, err := lockActor(ctx, q, orgID, actorID)
	if err != nil {
		return sqlc.User{}, Account{}, err
	}
	account, err := s.authorizePoolAccess(ctx, q, actorID, orgID, accountID, required)
	if err != nil {
		return sqlc.User{}, Account{}, err
	}
	return actor, account, nil
}

// authorizePoolAccess is the same live authority check for cheap admission and
// locked mutation. It performs no actor/organization locking on its own.
func (s *PoolService) authorizePoolAccess(ctx context.Context, q sqlc.Querier, actorID, orgID int64, accountID string, required Capabilities) (Account, error) {
	path, err := loadPath(ctx, q, orgID, accountID)
	if err != nil {
		return Account{}, err
	}
	caps, err := authorize(ctx, q, actorID, orgID, path, required)
	if err != nil {
		return Account{}, err
	}
	account := path[len(path)-1]
	info, infoErr := session.GetInfo(ctx)
	scope, scoped := ctx.Value(machineScopeKey{}).(machineScope)
	if infoErr == nil && (info.UserID != actorID || info.OrganizationID != orgID) {
		return Account{}, denied()
	}
	if scoped || (infoErr == nil && info.AccountKey) {
		if infoErr != nil || !info.AccountKey || info.AuthMethod != session.AuthMethodAPIKey || info.APIKeyDatabaseID <= 0 || s.keys == nil || !scoped || scope.accountID != account.AccountID {
			return Account{}, denied()
		}
		fresh, err := s.keys.authorize(ctx, q, info.APIKeyDatabaseID, actorID, orgID, account.AccountID)
		if err != nil {
			return Account{}, err
		}
		if fresh&caps&scope.capabilities&required != required {
			return Account{}, denied()
		}
	}
	return account, nil
}

func (s *PoolService) encryptSlots(options []PoolSlotOptions, previous []storedPoolSlot) ([]byte, error) {
	stored := make([]storedPoolSlot, len(options))
	for i, slot := range options {
		item := storedPoolSlot{URL: slot.URL, Username: slot.Username, WorkerTemplate: slot.WorkerTemplate}
		if slot.Password == nil && i < len(previous) {
			item.PasswordCiphertext, item.HasPassword = previous[i].PasswordCiphertext, previous[i].HasPassword
		} else {
			password := ""
			if slot.Password != nil {
				password = *slot.Password
			}
			var err error
			item.PasswordCiphertext, err = s.secrets.Encrypt([]byte(password))
			if err != nil {
				return nil, fleeterror.NewInternalError("encrypt pool password failed")
			}
			item.HasPassword = password != ""
		}
		stored[i] = item
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return nil, fleeterror.NewInternalError("encode pool slots failed")
	}
	return encoded, nil
}

func decodePoolSlots(encoded []byte) ([]storedPoolSlot, error) {
	var slots []storedPoolSlot
	if err := json.Unmarshal(encoded, &slots); err != nil {
		return nil, fleeterror.NewInternalError("decode pool slots failed")
	}
	return slots, nil
}

func publicPoolRevision(row sqlc.HostedAccountPoolRevision, archived bool) (PoolRevision, error) {
	stored, err := decodePoolSlots(row.Slots)
	if err != nil {
		return PoolRevision{}, err
	}
	result := PoolRevision{PoolID: row.PoolID, RevisionID: row.RevisionID, Name: row.Name, Number: row.Number, CreatedAt: row.CreatedAt, Archived: archived, Slots: make([]PoolEndpoint, len(stored))}
	for i, slot := range stored {
		result.Slots[i] = PoolEndpoint{Priority: int32(i), URL: slot.URL, Username: slot.Username, WorkerTemplate: slot.WorkerTemplate, HasPassword: slot.HasPassword} // #nosec G115 -- stored JSON slots are constrained to 1..3 ordered entries.
	}
	return result, nil
}

// Keep the cause available to the existing transaction retry machinery while
// its formatted message omits database values, including encrypted slots.
type poolStorageCause struct{ cause error }

func (e poolStorageCause) Error() string { return "database operation failed" }
func (e poolStorageCause) Unwrap() error { return e.cause }

func poolStorageError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return denied()
	}
	// Database errors can include JSON values containing ciphertext or usernames.
	return fleeterror.NewInternalErrorf("pool configuration storage failed: %w", poolStorageCause{err})
}

func validPoolID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.String() == id
}

func validatePoolOptions(options PoolRevisionOptions) (PoolRevisionOptions, error) {
	options.Name = strings.TrimSpace(options.Name)
	if options.Name == "" || !utf8.ValidString(options.Name) || utf8.RuneCountInString(options.Name) > 255 {
		return options, fleeterror.NewInvalidArgumentError("pool name must contain 1 to 255 characters")
	}
	if len(options.Slots) < 1 || len(options.Slots) > 3 {
		return options, fleeterror.NewInvalidArgumentError("pool configurations require 1 to 3 ordered slots")
	}
	for _, slot := range options.Slots {
		if !validPoolURL(slot.URL) {
			return options, fleeterror.NewInvalidArgumentError("invalid pool destination")
		}
		if !validPoolText(slot.Username, 255, false) || slot.Username == "" {
			return options, fleeterror.NewInvalidArgumentError("invalid payout username")
		}
		if slot.Password != nil && !validPoolText(*slot.Password, 255, true) {
			return options, fleeterror.NewInvalidArgumentError("invalid pool password")
		}
		if err := validateWorkerTemplate(slot.WorkerTemplate); err != nil {
			return options, err
		}
	}
	return options, nil
}

func validPoolText(value string, maxBytes int, allowSpace bool) bool {
	if len(value) > maxBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || (!allowSpace && unicode.IsSpace(r)) {
			return false
		}
	}
	return true
}

func validPoolURL(raw string) bool {
	if raw == "" || !validPoolText(raw, 256, false) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") || u.Opaque != "" || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "stratum+tcp", "stratum+ssl", "stratum+ws", "stratum2+tcp":
	default:
		return false
	}
	host := u.Hostname()
	if net.ParseIP(host) == nil {
		if len(host) == 0 || len(host) > 253 {
			return false
		}
		for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return false
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
					return false
				}
			}
		}
	}
	if strings.HasSuffix(u.Host, ":") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	if u.Scheme == "stratum2+tcp" {
		return u.Port() != "" && sv2.ValidatePoolURL(raw) == nil
	}
	return u.Path == "" || u.Path == "/"
}

func validateWorkerTemplate(template string) error {
	if !validPoolText(template, 128, false) {
		return fleeterror.NewInvalidArgumentError("invalid worker template")
	}
	for remaining := template; remaining != ""; {
		index := strings.IndexAny(remaining, "{}")
		if index < 0 {
			break
		}
		remaining = remaining[index:]
		switch {
		case strings.HasPrefix(remaining, "{miner_id}"):
			remaining = strings.TrimPrefix(remaining, "{miner_id}")
		case strings.HasPrefix(remaining, "{miner_name}"):
			remaining = strings.TrimPrefix(remaining, "{miner_name}")
		default:
			return fleeterror.NewInvalidArgumentError("invalid worker template placeholder")
		}
	}
	return nil
}

// RenderPoolWorker keeps the complete payout username, including its periods.
// Future allocation code supplies stable miner identifiers and names.
func RenderPoolWorker(username, template, minerID, minerName string) (string, error) {
	if username == "" || !validPoolText(username, 255, false) {
		return "", fleeterror.NewInvalidArgumentError("invalid payout username")
	}
	if err := validateWorkerTemplate(template); err != nil {
		return "", err
	}
	worker := username
	if template != "" {
		suffix := strings.NewReplacer("{miner_id}", minerID, "{miner_name}", minerName).Replace(template)
		worker += "." + suffix
	}
	if !validPoolText(worker, 255, false) {
		return "", fleeterror.NewInvalidArgumentError("rendered pool worker must contain at most 255 bytes without whitespace or controls")
	}
	return worker, nil
}
