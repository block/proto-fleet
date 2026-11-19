package hostedaccounts

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

type ReportingAuthority struct {
	UserID    string  `json:"userId"`
	Username  string  `json:"username,omitempty"`
	CanCreate bool    `json:"canCreate"`
	AllSites  bool    `json:"allSites"`
	SiteIDs   []int64 `json:"siteIds"`
	Revision  int64   `json:"revision"`
}
type reportingAccess struct {
	account     Account
	actor       sqlc.User
	grant       sqlc.HostedReportingAuthority
	permissions *authz.EffectivePermissions
	policy      Sites
}

func reportingHostAdmission(ctx context.Context, q sqlc.Querier, actorID, orgID int64, accountID, action string) (reportingAccess, error) {
	return reportingHostAdmissionMode(ctx, q, actorID, orgID, accountID, action, false)
}
func reportingHostReadAdmission(ctx context.Context, q sqlc.Querier, actorID, orgID int64, accountID, action string) (reportingAccess, error) {
	return reportingHostAdmissionMode(ctx, q, actorID, orgID, accountID, action, true)
}
func reportingHostAdmissionMode(ctx context.Context, q sqlc.Querier, actorID, orgID int64, accountID, action string, shared bool) (reportingAccess, error) {
	var out reportingAccess
	if _, selected := RequestAccountID(ctx); selected {
		return out, denied()
	}
	var actor sqlc.User
	var err error
	if shared {
		if _, err = q.LockHostedAccountOrganizationForShare(ctx, orgID); err == nil {
			actor, err = q.LockActiveHostedAccountUserForShare(ctx, sqlc.LockActiveHostedAccountUserForShareParams{ID: actorID, OrganizationID: orgID})
		}
		if err != nil {
			err = accessError(err)
		}
	} else {
		actor, err = lockActor(ctx, q, orgID, actorID)
	}
	if err != nil {
		return out, err
	}
	out.actor = actor
	path, err := loadPath(ctx, q, orgID, accountID)
	if err != nil {
		return out, err
	}
	out.account = path[len(path)-1]
	if out.account.Depth == 0 {
		return out, denied()
	}
	for _, a := range path {
		if a.Suspended {
			return out, denied()
		}
	}
	if shared {
		out.permissions, err = authz.LoadEffectiveForShare(ctx, q, actorID, orgID)
	} else {
		out.permissions, err = authz.LoadEffectiveForUpdate(ctx, q, actorID, orgID)
	}
	if err != nil {
		return out, err
	}
	if !out.permissions.HasAnyPermission() {
		return out, denied()
	}
	out.policy, err = sitePolicy(ctx, q, orgID, path, false)
	if err != nil {
		return out, err
	}
	if action == "admin" {
		if !out.permissions.Has(authz.PermAccountManage, authz.ResourceContext{}) {
			return out, denied()
		}
		return out, nil
	}
	out.grant, err = reportingGrant(ctx, q, out.permissions, orgID, out.account.ID, actorID)
	if err != nil {
		return out, err
	}
	if !out.grant.CanCreate {
		return out, denied()
	}
	return out, nil
}

// reportingGrant returns the user's reporting authority for one client.
// A stored grant is authoritative, including a narrowed or revoked one.
// Without one, account managers hold implicit all-site authority because
// they could grant it to themselves; per-site read permissions still apply.
func reportingGrant(ctx context.Context, q sqlc.Querier, permissions *authz.EffectivePermissions, orgID, accountID, userID int64) (sqlc.HostedReportingAuthority, error) {
	grant, err := q.GetReportingAuthority(ctx, sqlc.GetReportingAuthorityParams{OrganizationID: orgID, AccountID: accountID, UserID: userID})
	if !errors.Is(err, sql.ErrNoRows) {
		return grant, err
	}
	if !permissions.Has(authz.PermAccountManage, authz.ResourceContext{}) {
		return sqlc.HostedReportingAuthority{}, nil
	}
	return sqlc.HostedReportingAuthority{OrganizationID: orgID, AccountID: accountID, UserID: userID, CanCreate: true, AllSites: true}, nil
}
func (a reportingAccess) sitesAllowed(sites []int64) bool {
	if !a.hostSitesAllowed(sites) {
		return false
	}
	for _, site := range sites {
		if !siteAllowed(a.policy, site) {
			return false
		}
	}
	return true
}

// Historical removal requires host authority even when the account's current
// site policy no longer includes a removed site.
func (a reportingAccess) hostSitesAllowed(sites []int64) bool {
	for _, site := range sites {
		if site <= 0 || (!a.grant.AllSites && !slices.Contains(a.grant.SiteIds, site)) {
			return false
		}
		for _, permission := range []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead} {
			if !a.permissions.Has(permission, authz.ResourceContext{SiteID: &site}) {
				return false
			}
		}
	}
	return true
}
func (a reportingAccess) accountWideRequestsAllowed() bool {
	if !a.grant.AllSites || a.policy.EffectiveRestricted {
		return false
	}
	for _, permission := range []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead} {
		orgWide, exceptions := a.permissions.SiteScopeFor(permission)
		if !orgWide || len(exceptions) != 0 {
			return false
		}
	}
	return true
}

func reportingConflict() error {
	return fleeterror.NewFailedPreconditionError("reporting history changed; refresh before continuing")
}

func (s *Service) SetReportingAuthority(ctx context.Context, actorID, orgID int64, accountID string, grant ReportingAuthority, expected int64) error {
	if expected < 0 || grant.UserID == "" || len(grant.SiteIDs) > 1000 || (grant.AllSites && len(grant.SiteIDs) > 0) {
		return fleeterror.NewInvalidArgumentError("invalid reporting authority")
	}
	return db.WithTransactionTimeoutNoResult(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) error {
		access, err := reportingHostAdmission(ctx, q, actorID, orgID, accountID, "admin")
		if err != nil {
			return err
		}
		target, err := q.GetReportingUser(ctx, sqlc.GetReportingUserParams{UserID: grant.UserID, OrganizationID: orgID})
		if err != nil {
			return accessError(err)
		}
		previous, err := q.GetReportingAuthority(ctx, sqlc.GetReportingAuthorityParams{OrganizationID: orgID, AccountID: access.account.ID, UserID: target})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		sitesToCheck := slices.Clone(grant.SiteIDs)
		slices.Sort(sitesToCheck)
		sitesToCheck = slices.Compact(sitesToCheck)
		for start := 0; start < len(sitesToCheck); start += reportingValidationBatchSize {
			batch := sitesToCheck[start:min(start+reportingValidationBatchSize, len(sitesToCheck))]
			rows, err := q.ListReportingSites(ctx, sqlc.ListReportingSitesParams{Ids: batch, OrgID: orgID})
			if err != nil {
				return err
			}
			if len(rows) != len(batch) {
				return denied()
			}
			for _, row := range rows {
				if row.DeletedAt.Valid && !previous.AllSites && !slices.Contains(previous.SiteIds, row.ID) {
					return denied()
				}
			}
		}
		sites := sitesToCheck
		if sites == nil {
			sites = []int64{}
		}
		_, err = q.SetReportingAuthority(ctx, sqlc.SetReportingAuthorityParams{OrganizationID: orgID, AccountID: access.account.ID, UserID: target, CanCreate: grant.CanCreate, AllSites: grant.AllSites, SiteIds: sites, ExpectedRevision: expected})
		if errors.Is(err, sql.ErrNoRows) {
			return reportingConflict()
		}
		if err != nil {
			return err
		}
		return audit(ctx, q, access.actor, access.account, "set_reporting_authority", map[string]any{"user_id": grant.UserID, "previous": previous, "can_create": grant.CanCreate, "all_sites": grant.AllSites, "site_ids": sites})
	})
}

func (s *Service) ReportingAuthorities(ctx context.Context, actorID, orgID int64, accountID string) ([]ReportingAuthority, error) {
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) ([]ReportingAuthority, error) {
		a, err := reportingHostReadAdmission(ctx, q, actorID, orgID, accountID, "admin")
		if err != nil {
			return nil, err
		}
		rows, err := q.ListReportingAuthorities(ctx, sqlc.ListReportingAuthoritiesParams{OrganizationID: orgID, AccountID: a.account.ID})
		if err != nil {
			return nil, err
		}
		out := make([]ReportingAuthority, 0, len(rows))
		for _, row := range rows {
			out = append(out, ReportingAuthority{UserID: row.UserUuid, Username: row.Username, CanCreate: row.CanCreate, AllSites: row.AllSites, SiteIDs: row.SiteIds, Revision: row.Revision})
		}
		return out, nil
	})
}

const reportingValidationBatchSize = 500

func validateReportingExternalFacts(ctx context.Context, q sqlc.Querier, a reportingAccess, input reporting.Inputs) error {
	sites := input.Sites()
	if !a.hostSitesAllowed(sites) {
		return denied()
	}
	for _, site := range sites {
		if !siteAllowed(a.policy, site) {
			return fleeterror.NewInvalidArgumentErrorf("reporting site %d is excluded by the current account site policy", site)
		}
	}
	pairs := map[[2]int64]bool{}
	add := func(site, container int64) {
		if container > 0 {
			pairs[[2]int64{site, container}] = true
		}
	}
	for _, v := range input.Coverage {
		add(v.SiteID, v.ContainerID)
	}
	for _, v := range input.ModuleCoverage {
		add(v.SiteID, v.ContainerID)
	}
	for _, v := range input.Meters {
		add(v.SiteID, v.ContainerID)
	}
	for _, v := range input.Fees {
		add(v.SiteID, v.ContainerID)
	}
	for _, v := range input.Incidents {
		add(v.SiteID, v.ContainerID)
	}
	for _, v := range input.Hashrates {
		add(v.SiteID, v.ContainerID)
	}
	for _, v := range input.Obligations {
		add(v.SiteID, v.ContainerID)
	}
	for _, v := range input.ExternalResults {
		if v.ContainerID > 0 {
			add(v.SiteID, v.ContainerID)
		}
	}

	// Retained sites remain valid historical evidence after soft deletion.
	for start := 0; start < len(sites); start += reportingValidationBatchSize {
		batch := sites[start:min(start+reportingValidationBatchSize, len(sites))]
		rows, err := q.ListReportingSites(ctx, sqlc.ListReportingSitesParams{Ids: batch, OrgID: a.account.OrganizationID})
		if err != nil {
			return err
		}
		if len(rows) != len(batch) {
			return fleeterror.NewInvalidArgumentError("reporting site is outside the evidenced organization")
		}
	}
	type containerClaim struct {
		SiteID      int64 `json:"site_id"`
		ContainerID int64 `json:"container_id"`
	}
	claims := make([]containerClaim, 0, len(pairs))
	for pair := range pairs {
		claims = append(claims, containerClaim{pair[0], pair[1]})
	}
	slices.SortFunc(claims, func(a, b containerClaim) int {
		if order := cmp.Compare(a.SiteID, b.SiteID); order != 0 {
			return order
		}
		return cmp.Compare(a.ContainerID, b.ContainerID)
	})
	for start := 0; start < len(claims); start += reportingValidationBatchSize {
		batch := claims[start:min(start+reportingValidationBatchSize, len(claims))]
		payload, err := json.Marshal(batch)
		if err != nil {
			return fmt.Errorf("encode reporting container claims: %w", err)
		}
		matched, err := q.CountReportingContainers(ctx, sqlc.CountReportingContainersParams{Claims: payload, OrgID: a.account.OrganizationID})
		if err != nil {
			return err
		}
		if matched != int64(len(batch)) {
			return fleeterror.NewInvalidArgumentError("reporting container is outside the evidenced organization and site")
		}
	}
	if err := validateCoverageAllocations(ctx, q, a.account.OrganizationID, a.account.ID, input.Coverage); err != nil {
		return err
	}
	// Container ownership is checked separately from historical asset attribution.
	if err := validateReportingModules(ctx, q, a.account.OrganizationID, input); err != nil {
		return err
	}

	return nil
}

// validateCoverageAllocations rechecks commercial coverage against its retained
// allocation intervals, which recalls and placement changes can close later.
func validateCoverageAllocations(ctx context.Context, q sqlc.Querier, org, account int64, segments []reporting.CoverageSegment) error {
	intervalIDs := make([]int64, 0, len(segments))
	for _, segment := range segments {
		intervalIDs = append(intervalIDs, segment.AllocationIntervalID)
	}
	slices.Sort(intervalIDs)
	intervalIDs = slices.Compact(intervalIDs)
	intervals := make(map[int64]sqlc.HostedAccountAssetInterval, len(intervalIDs))
	// Bound each query independently of the number of facts in an account.
	for start := 0; start < len(intervalIDs); start += reportingValidationBatchSize {
		end := min(start+reportingValidationBatchSize, len(intervalIDs))
		rows, err := q.ListReportingAllocationIntervals(ctx, sqlc.ListReportingAllocationIntervalsParams{Ids: intervalIDs[start:end], OrganizationID: org, ClientID: account})
		if err != nil {
			return err
		}
		for _, row := range rows {
			intervals[row.ID] = row
		}
	}
	for _, coverage := range segments {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("validate reporting allocation: %w", err)
		}
		history, ok := intervals[coverage.AllocationIntervalID]
		if !ok {
			return denied()
		}
		if history.Kind != coverage.Kind || history.AssetID != coverage.AssetID || history.DeviceID != coverage.ContainerID || !history.SiteID.Valid || history.SiteID.Int64 != coverage.SiteID || coverage.Start.Before(history.StartedAt) || (history.EndedAt.Valid && (coverage.End.IsZero() || coverage.End.After(history.EndedAt.Time))) {
			return fleeterror.NewInvalidArgumentError("commercial coverage exceeds its evidenced allocation interval")
		}
	}
	return nil
}

type HostReportingAccount struct {
	AccountID     string `json:"accountId"`
	Name          string `json:"name"`
	CanCreate     bool   `json:"canCreate"`
	CanAdminister bool   `json:"canAdminister"`
}
type ReportingCaptureStream struct {
	SourceID      int16      `json:"sourceId"`
	Metric        int16      `json:"metric"`
	Expected      int        `json:"expected"`
	Missing       int        `json:"missing"`
	OldestCapture *time.Time `json:"oldestCapture,omitempty"`
	OldestSource  *time.Time `json:"oldestSource,omitempty"`
}
type ReportingSetup struct {
	CanManageRepresentatives bool                     `json:"canManageRepresentatives"`
	CanReadRequests          bool                     `json:"canReadRequests"`
	CanCreateRequests        bool                     `json:"canCreateRequests"`
	Owners                   []ReportingAuthority     `json:"owners"`
	AccountID                string                   `json:"accountId"`
	Name                     string                   `json:"name"`
	CanAdminister            bool                     `json:"canAdminister"`
	CanCreate                bool                     `json:"canCreate"`
	Authorities              []ReportingAuthority     `json:"authorities,omitempty"`
	Input                    *ReportingInputsMetadata `json:"input,omitempty"`
	Capture                  []ReportingCaptureStream `json:"capture"`
}

func (s *Service) HostReportingAccounts(ctx context.Context, actorID, orgID int64) ([]HostReportingAccount, error) {
	if _, selected := RequestAccountID(ctx); selected {
		return nil, denied()
	}
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) ([]HostReportingAccount, error) {
		if err := lockActorForRead(ctx, q, orgID, actorID); err != nil {
			return nil, err
		}
		permissions, err := authz.LoadEffectiveForShare(ctx, q, actorID, orgID)
		if err != nil {
			return nil, err
		}
		if !permissions.HasAnyPermission() {
			return nil, denied()
		}
		admin := permissions.Has(authz.PermAccountManage, authz.ResourceContext{})
		rows, err := q.ListHostReportingAccounts(ctx, sqlc.ListHostReportingAccountsParams{OrganizationID: orgID, UserID: actorID, Administrator: admin})
		if err != nil {
			return nil, err
		}
		out := make([]HostReportingAccount, 0, len(rows))
		for _, row := range rows {
			out = append(out, HostReportingAccount{AccountID: row.AccountID, Name: row.Name, CanCreate: admin || row.CanCreate, CanAdminister: admin})
		}
		return out, nil
	})
}
func (s *Service) HostReportingSetup(ctx context.Context, actorID, orgID int64, accountID string) (ReportingSetup, error) {
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) (ReportingSetup, error) {
		var out ReportingSetup
		a, err := reportingHostReadAdmission(ctx, q, actorID, orgID, accountID, "admin")
		admin := err == nil
		if !admin {
			a, err = reportingHostReadAdmission(ctx, q, actorID, orgID, accountID, "read")
			if err != nil {
				return out, err
			}
		}
		out.AccountID = a.account.AccountID
		out.Name = a.account.Name
		out.CanManageRepresentatives, err = reportingRepresentativeAuthority(ctx, q, actorID, orgID, accountID)
		if err != nil {
			return out, err
		}
		out.CanAdminister = admin
		grant, err := reportingGrant(ctx, q, a.permissions, orgID, a.account.ID, actorID)
		if err != nil {
			return out, err
		}
		a.grant = grant
		out.CanCreate = grant.CanCreate
		out.CanReadRequests = grant.CanCreate && a.accountWideRequestsAllowed()
		out.CanCreateRequests = grant.CanCreate && out.CanReadRequests
		if admin || grant.CanCreate {
			rows, err := q.ListReportingAuthorities(ctx, sqlc.ListReportingAuthoritiesParams{OrganizationID: orgID, AccountID: a.account.ID})
			if err != nil {
				return out, err
			}
			for _, row := range rows {
				if row.CanCreate {
					out.Owners = append(out.Owners, ReportingAuthority{UserID: row.UserUuid, Username: row.Username})
				}
				if admin {
					out.Authorities = append(out.Authorities, ReportingAuthority{UserID: row.UserUuid, Username: row.Username, CanCreate: row.CanCreate, AllSites: row.AllSites, SiteIDs: row.SiteIds, Revision: row.Revision})
				}
			}
			managers, err := q.ListReportingAccountManagers(ctx, orgID)
			if err != nil {
				return out, err
			}
			for _, manager := range managers {
				// A stored grant, even a revoked one, overrides implicit authority.
				if !slices.ContainsFunc(rows, func(row sqlc.ListReportingAuthoritiesRow) bool { return row.UserID == manager.ID }) {
					out.Owners = append(out.Owners, ReportingAuthority{UserID: manager.UserUuid, Username: manager.Username})
				}
			}
			slices.SortFunc(out.Owners, func(a, b ReportingAuthority) int { return cmp.Compare(a.UserID, b.UserID) })
		}
		if grant.CanCreate {
			row, err := q.LockReportingInputsForRead(ctx, sqlc.LockReportingInputsForReadParams{OrganizationID: orgID, AccountID: a.account.ID})
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return out, err
			}
			if err == nil && a.hostSitesAllowed(row.SiteIds) {
				input, err := reportingInputsMetadata(ctx, q, row)
				if err != nil {
					return out, err
				}
				out.Input = &input
			}
			streams, err := q.ListReportingCaptureStreams(ctx, sqlc.ListReportingCaptureStreamsParams{OrganizationID: orgID, ClientID: a.account.ID})
			if err != nil {
				return out, err
			}
			grouped := map[[2]int16]*ReportingCaptureStream{}
			for _, row := range streams {
				if !row.SiteID.Valid || !a.sitesAllowed([]int64{row.SiteID.Int64}) {
					continue
				}
				key := [2]int16{row.SourceID, row.Metric}
				g := grouped[key]
				if g == nil {
					g = &ReportingCaptureStream{SourceID: row.SourceID, Metric: row.Metric}
					grouped[key] = g
				}
				g.Expected++
				if !row.HasObservation || !row.LastObservationCapturedAt.Valid || !row.LastObservationSourceNewestAt.Valid {
					g.Missing++
					continue
				}
				capture, source := row.LastObservationCapturedAt.Time, row.LastObservationSourceNewestAt.Time
				if g.OldestCapture == nil || capture.Before(*g.OldestCapture) {
					g.OldestCapture = &capture
				}
				if g.OldestSource == nil || source.Before(*g.OldestSource) {
					g.OldestSource = &source
				}
			}
			for _, g := range grouped {
				out.Capture = append(out.Capture, *g)
			}
			slices.SortFunc(out.Capture, func(a, b ReportingCaptureStream) int {
				if a.SourceID != b.SourceID {
					return int(a.SourceID - b.SourceID)
				}
				return int(a.Metric - b.Metric)
			})
		}
		return out, nil
	})
}

func reportingRepresentativeAuthority(ctx context.Context, q sqlc.Querier, actorID, orgID int64, accountID string) (bool, error) {
	path, err := loadPath(ctx, q, orgID, accountID)
	if err != nil {
		return false, err
	}
	_, err = authorizeForRead(ctx, q, actorID, orgID, path, ManageMembers)
	if fleeterror.IsForbiddenError(err) {
		return false, nil
	}
	return err == nil, err
}
func sameReportingRepresentatives(a, b []reporting.Representative) bool {
	if len(a) != len(b) {
		return false
	}
	refs := make(map[string]reporting.Representative, len(a))
	for _, v := range a {
		refs[v.UserID] = v
	}
	for _, v := range b {
		ref, ok := refs[v.UserID]
		if !ok || ref != v {
			return false
		}
	}
	return true
}
