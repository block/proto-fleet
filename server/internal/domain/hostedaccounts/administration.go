package hostedaccounts

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// These DTOs expose stable public account and user identities. Internal database
// keys and unrelated memberships never leave the account administration boundary.
type AccountSummary struct {
	AccountID             string
	ParentAccountID       string
	Name                  string
	Depth                 int32
	Capabilities          Capabilities
	Suspended             bool
	Revision              int64
	EffectiveCapabilities Capabilities
	Role                  Role
}

type Member struct {
	UserID   string
	Username string
	Role     Role
	Revision int64
	JoinedAt time.Time
}

type AncestorGrant struct {
	AncestorAccountID string
	Capabilities      Capabilities
	Revision          int64
}

type Sites struct {
	Restricted          bool
	SiteIDs             []int64
	EffectiveRestricted bool
	EffectiveSiteIDs    []int64
}

func summary(a Account, path []Account) AccountSummary {
	result := AccountSummary{AccountID: a.AccountID, Name: a.Name, Depth: a.Depth, Capabilities: Capabilities(a.Capabilities), Suspended: a.Suspended, Revision: a.Revision}
	if len(path) > 1 {
		result.ParentAccountID = path[len(path)-2].AccountID
	}
	return result
}

func (s *Service) ListAccounts(ctx context.Context, actorID, orgID int64) ([]AccountSummary, error) {
	accounts, err := s.queries.ListAuthorizedHostedAccounts(ctx, sqlc.ListAuthorizedHostedAccountsParams{UserID: actorID, OrganizationID: orgID})
	if err != nil {
		return nil, err
	}
	result := make([]AccountSummary, 0, len(accounts))
	for _, a := range accounts {
		path, err := loadPath(ctx, s.queries, orgID, a.AccountID)
		if err != nil {
			return nil, err
		}
		item := summary(a, path)
		if a.Depth == 0 {
			item.EffectiveCapabilities = Delegate
		} else {
			access, err := resolve(ctx, s.queries, actorID, orgID, a.AccountID)
			if err != nil {
				return nil, err
			}
			item.EffectiveCapabilities = access.Capabilities
			item.Role = access.Role
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Service) GetAccount(ctx context.Context, actorID, orgID int64, accountID string) (AccountSummary, error) {
	path, err := loadPath(ctx, s.queries, orgID, accountID)
	if err != nil {
		return AccountSummary{}, err
	}
	access, err := resolve(ctx, s.queries, actorID, orgID, accountID)
	if err != nil {
		return AccountSummary{}, err
	}
	item := summary(path[len(path)-1], path)
	item.EffectiveCapabilities = access.Capabilities
	item.Role = access.Role
	return item, nil
}

// Delegation allows the parent's account policy list, never a child's private
// membership roster, pool credentials, assets, or telemetry.
func (s *Service) ListChildren(ctx context.Context, actorID, orgID int64, parentID string) ([]AccountSummary, error) {
	path, err := loadPath(ctx, s.queries, orgID, parentID)
	if err != nil {
		return nil, err
	}
	if _, err = authorize(ctx, s.queries, actorID, orgID, path, Delegate); err != nil {
		return nil, err
	}
	parent := path[len(path)-1]
	children, err := s.queries.ListHostedAccountChildren(ctx, sqlc.ListHostedAccountChildrenParams{OrganizationID: orgID, ParentID: sql.NullInt64{Int64: parent.ID, Valid: true}})
	if err != nil {
		return nil, err
	}
	result := make([]AccountSummary, 0, len(children))
	for _, child := range children {
		item := summary(child, append(path, child))
		access, err := resolve(ctx, s.queries, actorID, orgID, child.AccountID)
		if err != nil && !fleeterror.IsForbiddenError(err) {
			return nil, err
		}
		if err == nil {
			item.EffectiveCapabilities = access.Capabilities
			item.Role = access.Role
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Service) ListMembers(ctx context.Context, actorID, orgID int64, accountID string) ([]Member, error) {
	path, err := loadPath(ctx, s.queries, orgID, accountID)
	if err != nil {
		return nil, err
	}
	if _, err = authorize(ctx, s.queries, actorID, orgID, path, ManageMembers); err != nil {
		return nil, err
	}
	rows, err := s.queries.ListHostedAccountMembers(ctx, sqlc.ListHostedAccountMembersParams{OrganizationID: orgID, AccountID: path[len(path)-1].ID})
	if err != nil {
		return nil, err
	}
	members := make([]Member, 0, len(rows))
	for _, row := range rows {
		members = append(members, Member{UserID: row.UserID, Username: row.Username, Role: Role(row.Role), Revision: row.Revision, JoinedAt: row.CreatedAt})
	}
	return members, nil
}

// FindMember resolves only an existing member of the selected account. New
// members enter through invitation acceptance, not a global user directory.
func (s *Service) FindMember(ctx context.Context, actorID, orgID int64, accountID, userID string) (int64, error) {
	path, err := loadPath(ctx, s.queries, orgID, accountID)
	if err != nil {
		return 0, err
	}
	if _, err = authorize(ctx, s.queries, actorID, orgID, path, ManageMembers); err != nil {
		return 0, err
	}
	id, err := s.queries.FindHostedAccountMemberByUUID(ctx, sqlc.FindHostedAccountMemberByUUIDParams{OrganizationID: orgID, AccountID: path[len(path)-1].ID, UserID: userID})
	if err != nil {
		return 0, accessError(err)
	}
	return id, nil
}

func (s *Service) Rename(ctx context.Context, actorID, orgID int64, accountID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 255 {
		return fleeterror.NewInvalidArgumentError("account name must contain 1 to 255 characters")
	}
	return db.WithTransactionNoResult(ctx, s.conn, func(q sqlc.Querier) error {
		actor, err := lockActor(ctx, q, orgID, actorID)
		if err != nil {
			return err
		}
		path, err := loadPath(ctx, q, orgID, accountID)
		if err != nil {
			return err
		}
		if _, err = authorize(ctx, q, actorID, orgID, path, ManageMembers); err != nil {
			return err
		}
		a := path[len(path)-1]
		if err = q.RenameHostedAccount(ctx, sqlc.RenameHostedAccountParams{Name: name, OrganizationID: orgID, ID: a.ID}); err != nil {
			return err
		}
		a.Revision++
		return audit(ctx, q, actor, a, "rename_hosted_account", map[string]any{"previous_name": a.Name, "name": name})
	})
}

func grantAuthority(ctx context.Context, q sqlc.Querier, actorID, orgID int64, path []Account) (Capabilities, error) {
	// Host policy administration may explicitly grant access. It confers no
	// private access before that grant is written and audited.
	live, err := q.HasHostedAccountHostAuthority(ctx, sqlc.HasHostedAccountHostAuthorityParams{UserID: actorID, OrganizationID: orgID})
	if err != nil {
		return 0, err
	}
	effective, err := authz.LoadEffectiveForUpdate(ctx, q, actorID, orgID)
	if err != nil {
		return 0, err
	}
	_, customerRequest := RequestAccountID(ctx)
	if !customerRequest && live && effective.Has(authz.PermAccountManage, authz.ResourceContext{}) {
		ceiling := AllCapabilities
		for _, a := range path {
			ceiling &= Capabilities(a.Capabilities)
		}
		return ceiling, nil
	}
	return authorize(ctx, q, actorID, orgID, path, ManageMembers)
}

func ancestor(path []Account, accountID string) (Account, error) {
	for _, a := range path[:len(path)-1] {
		if a.AccountID == accountID {
			return a, nil
		}
	}
	return Account{}, fleeterror.NewInvalidArgumentError("grant must name a strict ancestor of the account")
}

func (s *Service) ListAncestorGrants(ctx context.Context, actorID, orgID int64, accountID string) ([]AncestorGrant, error) {
	path, err := loadPath(ctx, s.queries, orgID, accountID)
	if err != nil {
		return nil, err
	}
	if _, err = grantAuthority(ctx, s.queries, actorID, orgID, path); err != nil {
		return nil, err
	}
	rows, err := s.queries.ListHostedAccountAncestorGrants(ctx, sqlc.ListHostedAccountAncestorGrantsParams{OrganizationID: orgID, AccountID: path[len(path)-1].ID})
	if err != nil {
		return nil, err
	}
	result := make([]AncestorGrant, 0, len(rows))
	for _, g := range rows {
		for _, a := range path {
			if a.ID == g.AncestorID {
				result = append(result, AncestorGrant{AncestorAccountID: a.AccountID, Capabilities: Capabilities(g.Capabilities), Revision: g.Revision})
			}
		}
	}
	return result, nil
}

func (s *Service) SetAncestorGrant(ctx context.Context, actorID, orgID int64, accountID, ancestorID string, capabilities Capabilities) error {
	if capabilities&View == 0 || capabilities&^AllCapabilities != 0 {
		return fleeterror.NewInvalidArgumentError("ancestor grant must include viewing and only defined capabilities")
	}
	return s.changeAncestorGrant(ctx, actorID, orgID, accountID, ancestorID, &capabilities)
}

func (s *Service) RevokeAncestorGrant(ctx context.Context, actorID, orgID int64, accountID, ancestorID string) error {
	return s.changeAncestorGrant(ctx, actorID, orgID, accountID, ancestorID, nil)
}

func (s *Service) changeAncestorGrant(ctx context.Context, actorID, orgID int64, accountID, ancestorID string, capabilities *Capabilities) error {
	return db.WithTransactionNoResult(ctx, s.conn, func(q sqlc.Querier) error {
		actor, err := lockActor(ctx, q, orgID, actorID)
		if err != nil {
			return err
		}
		path, err := loadPath(ctx, q, orgID, accountID)
		if err != nil {
			return err
		}
		ceiling, err := grantAuthority(ctx, q, actorID, orgID, path)
		if err != nil {
			return err
		}
		source, err := ancestor(path, ancestorID)
		if err != nil {
			return err
		}
		a := path[len(path)-1]
		event := "revoke_hosted_account_ancestor_grant"
		metadata := map[string]any{"ancestor_account_id": source.AccountID}
		if capabilities != nil {
			if *capabilities&^ceiling != 0 {
				return fleeterror.NewForbiddenError("cannot grant capabilities outside current authority")
			}
			err = q.SetHostedAccountAncestorGrant(ctx, sqlc.SetHostedAccountAncestorGrantParams{OrganizationID: orgID, AccountID: a.ID, AncestorID: source.ID, Capabilities: int64(*capabilities)})
			metadata["capabilities"] = *capabilities
			event = "set_hosted_account_ancestor_grant"
		} else {
			if _, err = q.GetHostedAccountAncestorGrant(ctx, sqlc.GetHostedAccountAncestorGrantParams{OrganizationID: orgID, AccountID: a.ID, AncestorID: source.ID}); err != nil {
				return accessError(err)
			}
			err = q.RevokeHostedAccountAncestorGrant(ctx, sqlc.RevokeHostedAccountAncestorGrantParams{OrganizationID: orgID, AccountID: a.ID, AncestorID: source.ID})
		}
		if err != nil {
			return err
		}
		if err = requireAdministrators(ctx, q, orgID); err != nil {
			return err
		}
		return audit(ctx, q, actor, a, event, metadata)
	})
}

func (s *Service) SitePolicy(ctx context.Context, actorID, orgID int64, accountID string) (Sites, error) {
	path, err := loadPath(ctx, s.queries, orgID, accountID)
	if err != nil {
		return Sites{}, err
	}
	parentControlled, err := sitePolicyAuthority(ctx, s.queries, actorID, orgID, path, View)
	if err != nil {
		return Sites{}, err
	}
	return sitePolicy(ctx, s.queries, orgID, path, parentControlled)
}

// The selected workspace chooses the independently editable policy. A parent
// always controls its child's ceiling, even when it also has private authority.
func sitePolicyAuthority(ctx context.Context, q sqlc.Querier, actorID, orgID int64, path []Account, required Capabilities) (bool, error) {
	if len(path) == 1 {
		_, err := authorize(ctx, q, actorID, orgID, path, Delegate)
		return false, err
	}
	selected, scoped := RequestAccountID(ctx)
	parent := path[:len(path)-1]
	if scoped && selected == parent[len(parent)-1].AccountID {
		_, err := authorize(ctx, q, actorID, orgID, parent, Delegate)
		return true, err
	}
	if scoped && selected != path[len(path)-1].AccountID {
		return false, denied()
	}
	_, err := authorize(ctx, q, actorID, orgID, path, required)
	if err == nil || scoped {
		return false, err
	}
	_, err = authorize(ctx, q, actorID, orgID, parent, Delegate)
	return true, err
}

func sitePolicy(ctx context.Context, q sqlc.Querier, orgID int64, path []Account, parentControlled bool) (Sites, error) {
	target := path[len(path)-1]
	result := Sites{Restricted: target.SitesRestricted, SiteIDs: []int64{}, EffectiveSiteIDs: []int64{}}
	if parentControlled {
		result.Restricted = target.SiteCeilingRestricted
	}
	var allowed map[int64]bool
	for _, a := range path {
		if !a.SitesRestricted && !a.SiteCeilingRestricted && a.ID != target.ID {
			continue
		}
		rows, err := q.ListHostedAccountSites(ctx, sqlc.ListHostedAccountSitesParams{OrganizationID: orgID, AccountID: a.ID})
		if err != nil {
			return Sites{}, err
		}
		ids := make(map[int64]bool, len(rows))
		ceilingIDs := make(map[int64]bool, len(rows))
		for _, row := range rows {
			if row.ParentControlled {
				ceilingIDs[row.SiteID] = true
			} else {
				ids[row.SiteID] = true
			}
			if a.ID == target.ID && row.ParentControlled == parentControlled {
				result.SiteIDs = append(result.SiteIDs, row.SiteID)
			}
		}
		for _, policy := range []struct {
			restricted bool
			ids        map[int64]bool
		}{{a.SitesRestricted, ids}, {a.SiteCeilingRestricted, ceilingIDs}} {
			if !policy.restricted {
				continue
			}
			result.EffectiveRestricted = true
			if allowed == nil {
				allowed = policy.ids
			} else {
				for id := range allowed {
					if !policy.ids[id] {
						delete(allowed, id)
					}
				}
			}
		}
	}
	for id := range allowed {
		result.EffectiveSiteIDs = append(result.EffectiveSiteIDs, id)
	}
	slices.Sort(result.SiteIDs)
	slices.Sort(result.EffectiveSiteIDs)
	return result, nil
}

func (s *Service) ConfigureSites(ctx context.Context, actorID, orgID int64, accountID string, restricted bool, siteIDs []int64) error {
	if !restricted && len(siteIDs) > 0 {
		return fleeterror.NewInvalidArgumentError("unrestricted site policy must not contain a site list")
	}
	return db.WithTransactionNoResult(ctx, s.conn, func(q sqlc.Querier) error {
		actor, err := lockActor(ctx, q, orgID, actorID)
		if err != nil {
			return err
		}
		path, err := loadPath(ctx, q, orgID, accountID)
		if err != nil {
			return err
		}
		a := path[len(path)-1]
		parentControlled, err := sitePolicyAuthority(ctx, q, actorID, orgID, path, ManageMembers)
		if err != nil {
			return err
		}
		if err = q.DeleteHostedAccountSites(ctx, sqlc.DeleteHostedAccountSitesParams{OrganizationID: orgID, AccountID: a.ID, ParentControlled: parentControlled}); err != nil {
			return err
		}
		seen := make(map[int64]bool, len(siteIDs))
		for _, id := range siteIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			n, err := q.AddHostedAccountSite(ctx, sqlc.AddHostedAccountSiteParams{OrganizationID: orgID, AccountID: a.ID, ID: id, ParentControlled: parentControlled})
			if err != nil {
				return err
			}
			if n != 1 {
				return fleeterror.NewInvalidArgumentError("site must be active and belong to the hosting organization")
			}
		}
		if err = q.SetHostedAccountSitesRestricted(ctx, sqlc.SetHostedAccountSitesRestrictedParams{Restricted: restricted, OrganizationID: orgID, ID: a.ID, ParentControlled: parentControlled}); err != nil {
			return err
		}
		a.Revision++
		return audit(ctx, q, actor, a, "configure_hosted_account_sites", map[string]any{"restricted": restricted, "site_ids": siteIDs, "parent_controlled": parentControlled})
	})
}

func requireAdministrators(ctx context.Context, q sqlc.Querier, orgID int64) error {
	count, err := q.CountHostedAccountsWithoutAdministrator(ctx, sqlc.CountHostedAccountsWithoutAdministratorParams{OrganizationID: orgID, ExcludedUserID: 0})
	if err != nil {
		return err
	}
	if count > 0 {
		return fleeterror.NewFailedPreconditionError("account must retain an active direct or explicitly authorized ancestor administrator")
	}
	return nil
}

// Invocation occurs after the writer's organization lock. The SQL witness
// includes the live membership/grant source and policy generation, not labels.
func authorityFingerprint(ctx context.Context, q sqlc.Querier, actorID, orgID int64, path []Account) (string, error) {
	row, err := q.ResolveHostedAccountAuthority(ctx, sqlc.ResolveHostedAccountAuthorityParams{OrganizationID: orgID, AccountID: path[len(path)-1].AccountID, UserID: actorID})
	if err != nil {
		return "", accessError(err)
	}
	if !(Access{Capabilities: Capabilities(row.Capabilities)}).Has(ManageMembers) {
		return "", denied()
	}
	digest := sha256.Sum256([]byte(row.AuthorityState))
	return hex.EncodeToString(digest[:]), nil
}
