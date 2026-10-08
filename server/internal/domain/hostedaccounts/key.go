package hostedaccounts

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
)

const MachineCapabilities = View | ManagePools | ChangePools

type KeyOptions struct {
	Name                     string
	Capabilities             Capabilities
	DescendantAccountIDs     []string
	IncludeFutureDescendants bool
	ExpiresAt                *time.Time
}

// AccountKey contains public lifecycle and scope metadata, never its secret/hash.
type AccountKey = sqlc.ListHostedAccountKeysRow

type IssuedAccountKey struct {
	Key    AccountKey
	Secret string
}

type KeyService struct {
	accounts    *Service
	credentials *apikey.Service
}

func NewKeyService(accounts *Service, credentials *apikey.Service) *KeyService {
	return &KeyService{accounts: accounts, credentials: credentials}
}

// Create permits personal keys within the issuer's current customer authority.
// Account administrators can subsequently list and revoke all keys of that account.
func (s *KeyService) Create(ctx context.Context, actorID, orgID int64, accountID string, options KeyOptions) (IssuedAccountKey, error) {
	if len(options.DescendantAccountIDs) > 100 {
		return IssuedAccountKey{}, fleeterror.NewInvalidArgumentError("account keys may select at most 100 descendants")
	}
	options.Name = strings.TrimSpace(options.Name)
	if options.Name == "" || !utf8.ValidString(options.Name) || utf8.RuneCountInString(options.Name) > 255 {
		return IssuedAccountKey{}, fleeterror.NewInvalidArgumentError("key name must contain 1 to 255 characters")
	}
	if options.Capabilities&View == 0 || options.Capabilities&^MachineCapabilities != 0 {
		return IssuedAccountKey{}, fleeterror.NewInvalidArgumentError("machine keys require viewing and may permit pool configuration and changes")
	}
	if options.ExpiresAt != nil && !options.ExpiresAt.After(time.Now()) {
		return IssuedAccountKey{}, fleeterror.NewInvalidArgumentError("key expiration must be in the future")
	}
	return db.WithTransaction(ctx, s.accounts.conn, func(q sqlc.Querier) (IssuedAccountKey, error) {
		actor, err := lockActor(ctx, q, orgID, actorID)
		if err != nil {
			return IssuedAccountKey{}, err
		}
		path, err := loadPath(ctx, q, orgID, accountID)
		if err != nil {
			return IssuedAccountKey{}, err
		}
		effective, err := authorize(ctx, q, actorID, orgID, path, View)
		if err != nil {
			return IssuedAccountKey{}, err
		}
		account := path[len(path)-1]
		if account.Depth == 0 || options.Capabilities&^effective != 0 {
			return IssuedAccountKey{}, denied()
		}
		if options.IncludeFutureDescendants && account.Depth == 2 {
			return IssuedAccountKey{}, fleeterror.NewInvalidArgumentError("subclients cannot have future descendants")
		}
		descendants := make([]Account, 0, len(options.DescendantAccountIDs))
		seen := map[string]bool{}
		for _, id := range options.DescendantAccountIDs {
			id, err = canonicalAccountID(id)
			if err != nil {
				return IssuedAccountKey{}, err
			}
			if seen[id] {
				return IssuedAccountKey{}, fleeterror.NewInvalidArgumentError("duplicate descendant account")
			}
			seen[id] = true
			childPath, err := loadPath(ctx, q, orgID, id)
			if err != nil {
				return IssuedAccountKey{}, err
			}
			child := childPath[len(childPath)-1]
			if !child.ParentID.Valid || child.ParentID.Int64 != account.ID {
				return IssuedAccountKey{}, denied()
			}
			if _, err = authorize(ctx, q, actorID, orgID, childPath, View); err != nil {
				return IssuedAccountKey{}, err
			}
			descendants = append(descendants, child)
		}
		txCtx := db.WithTxQueries(ctx, q)
		secret, _, err := s.credentials.CreateAccount(txCtx, actorID, orgID, options.Name, options.ExpiresAt)
		if err != nil {
			return IssuedAccountKey{}, err
		}
		key, err := s.credentials.Validate(txCtx, secret)
		if err != nil {
			return IssuedAccountKey{}, err
		}
		if err = q.BindHostedAccountKey(ctx, sqlc.BindHostedAccountKeyParams{ApiKeyID: key.ID, OrganizationID: orgID, AccountID: account.ID, Capabilities: int64(options.Capabilities), IncludeFutureDescendants: options.IncludeFutureDescendants}); err != nil {
			return IssuedAccountKey{}, err
		}
		ids := make([]string, 0, len(descendants))
		for _, child := range descendants {
			if err = q.AddHostedAccountKeyDescendant(ctx, sqlc.AddHostedAccountKeyDescendantParams{OrganizationID: orgID, ApiKeyID: key.ID, AccountID: child.ID}); err != nil {
				return IssuedAccountKey{}, err
			}
			ids = append(ids, child.AccountID)
		}
		slices.Sort(ids)
		if err = audit(ctx, q, actor, account, "create_hosted_account_key", map[string]any{"key_id": key.KeyID, "capabilities": options.Capabilities, "descendant_account_ids": ids, "include_future_descendants": options.IncludeFutureDescendants}); err != nil {
			return IssuedAccountKey{}, err
		}
		item := AccountKey{KeyID: key.KeyID, Name: key.Name, Prefix: key.Prefix, CreatedAt: key.CreatedAt, IssuerUserID: actor.UserID, IssuerUsername: actor.Username, Capabilities: int64(options.Capabilities), DescendantAccountIds: ids, IncludeFutureDescendants: options.IncludeFutureDescendants}
		if key.ExpiresAt != nil {
			item.ExpiresAt = sql.NullTime{Time: *key.ExpiresAt, Valid: true}
		}
		return IssuedAccountKey{Key: item, Secret: secret}, nil
	})
}

// Authorize rechecks the credential, explicit bounds, and live issuer authority
// on both the issuing account and selected target. A descendant bound grants no
// account membership. The future-descendant opt-in includes all current and future
// direct children, each of which still needs live issuer access.
func (s *KeyService) Authorize(ctx context.Context, keyID, issuerID, orgID int64, targetID string) (Capabilities, error) {
	ctx = context.WithValue(ctx, machineScopeKey{}, nil)
	q := s.accounts.queries
	if tx := db.GetTxQueries(ctx); tx != nil {
		q = tx
	}
	key, err := q.GetHostedAccountKeyAuthority(ctx, sqlc.GetHostedAccountKeyAuthorityParams{ApiKeyID: keyID, OrganizationID: orgID, UserID: sql.NullInt64{Int64: issuerID, Valid: true}})
	if err != nil {
		return 0, accessError(err)
	}
	base, err := resolve(ctx, q, issuerID, orgID, key.AccountUuid)
	if err != nil {
		return 0, err
	}
	path, err := loadPath(ctx, q, orgID, targetID)
	if err != nil {
		return 0, err
	}
	target := path[len(path)-1]
	if target.ID != key.AccountID {
		if !target.ParentID.Valid || target.ParentID.Int64 != key.AccountID || (!key.IncludeFutureDescendants && !slices.Contains(key.DescendantIds, target.ID)) {
			return 0, denied()
		}
	}
	access, err := resolve(ctx, q, issuerID, orgID, target.AccountID)
	if err != nil {
		return 0, err
	}
	caps := Capabilities(key.Capabilities) & base.Capabilities & access.Capabilities
	if caps&View == 0 {
		return 0, denied()
	}
	return caps, nil
}

// List rechecks live authority on every page. A continuation cannot change the
// issuer or widen an own-key listing into an administrator's listing.
func (s *KeyService) List(ctx context.Context, actorID, orgID int64, accountID string, pageSize int32, pageToken string) ([]AccountKey, string, error) {
	if pageSize < 0 || pageSize > 100 {
		return nil, "", fleeterror.NewInvalidArgumentError("key page size must be between 0 and 100")
	}
	if pageSize == 0 {
		pageSize = 25
	}
	path, err := loadPath(ctx, s.accounts.queries, orgID, accountID)
	if err != nil {
		return nil, "", err
	}
	access, err := resolve(ctx, s.accounts.queries, actorID, orgID, accountID)
	if err != nil {
		return nil, "", err
	}
	includeAll := access.Has(ManageMembers)
	cursorTime, cursorID, err := accountKeyPageCursor(pageToken, actorID, orgID, accountID, includeAll)
	if err != nil {
		return nil, "", err
	}
	rows, err := s.accounts.queries.ListHostedAccountKeys(ctx, sqlc.ListHostedAccountKeysParams{
		OrganizationID: orgID, AccountID: path[len(path)-1].ID, UserID: sql.NullInt64{Int64: actorID, Valid: true}, IncludeAll: includeAll,
		CursorTime: cursorTime, CursorID: cursorID, PageLimit: pageSize + 1,
	})
	if err != nil {
		return nil, "", err
	}
	if len(rows) <= int(pageSize) {
		return rows, "", nil
	}
	rows = rows[:pageSize]
	return rows, accountKeyPageToken(actorID, orgID, accountID, includeAll, rows[len(rows)-1]), nil
}

func accountKeyPageToken(actorID, orgID int64, accountID string, includeAll bool, last AccountKey) string {
	payload := fmt.Sprintf("1|%d|%s|%d|%t|%s|%s", orgID, accountID, actorID, includeAll, last.CreatedAt.UTC().Format(time.RFC3339Nano), last.KeyID)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func accountKeyPageCursor(token string, actorID, orgID int64, accountID string, includeAll bool) (sql.NullTime, string, error) {
	if token == "" {
		return sql.NullTime{}, "", nil
	}
	invalid := fleeterror.NewInvalidArgumentError("invalid key page token for this account and listing")
	if len(token) > 512 {
		return sql.NullTime{}, "", invalid
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return sql.NullTime{}, "", invalid
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 7 || parts[0] != "1" || parts[1] != strconv.FormatInt(orgID, 10) || parts[2] != accountID || parts[3] != strconv.FormatInt(actorID, 10) || parts[4] != strconv.FormatBool(includeAll) {
		return sql.NullTime{}, "", invalid
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[5])
	if err != nil || createdAt.IsZero() {
		return sql.NullTime{}, "", invalid
	}
	id, err := uuid.Parse(parts[6])
	if err != nil || id.String() != parts[6] {
		return sql.NullTime{}, "", invalid
	}
	return sql.NullTime{Time: createdAt, Valid: true}, parts[6], nil
}

func (s *KeyService) Revoke(ctx context.Context, actorID, orgID int64, accountID, keyID string) error {
	err := db.WithTransactionNoResult(ctx, s.accounts.conn, func(q sqlc.Querier) error {
		actor, err := lockActor(ctx, q, orgID, actorID)
		if err != nil {
			return err
		}
		path, err := loadPath(ctx, q, orgID, accountID)
		if err != nil {
			return err
		}
		caps, err := authorize(ctx, q, actorID, orgID, path, View)
		if err != nil {
			return err
		}
		account := path[len(path)-1]
		owner, err := q.GetHostedAccountKeyOwner(ctx, sqlc.GetHostedAccountKeyOwnerParams{KeyID: keyID, OrganizationID: orgID, AccountID: account.ID})
		if err != nil {
			return accessError(err)
		}
		if !owner.Valid || (owner.Int64 != actorID && caps&ManageMembers == 0) {
			return denied()
		}
		n, err := q.RevokeHostedAccountKey(ctx, sqlc.RevokeHostedAccountKeyParams{KeyID: keyID, OrganizationID: orgID, AccountID: account.ID})
		if err != nil {
			return err
		}
		if n != 1 {
			return denied()
		}
		return audit(ctx, q, actor, account, "revoke_hosted_account_key", map[string]any{"key_id": keyID})
	})
	if err == nil {
		s.credentials.ForgetRevokedKey(keyID)
	}
	return err
}

type machineScopeKey struct{}
type machineScope struct {
	accountID    string
	capabilities Capabilities
}

// WithMachineScope is used only after the account credential has been authorized.
// It limits every downstream account resolution without introducing host permissions.
func WithMachineScope(ctx context.Context, accountID string, capabilities Capabilities) (context.Context, error) {
	if capabilities&View == 0 || capabilities&^MachineCapabilities != 0 {
		return ctx, denied()
	}
	id, err := canonicalAccountID(accountID)
	if err != nil {
		return ctx, err
	}
	if old, ok := ctx.Value(machineScopeKey{}).(machineScope); ok && (old.accountID != id || capabilities&^old.capabilities != 0) {
		return ctx, denied()
	}
	return context.WithValue(ctx, machineScopeKey{}, machineScope{id, capabilities}), nil
}
