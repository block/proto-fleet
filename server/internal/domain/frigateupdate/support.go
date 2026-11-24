package frigateupdate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Support is the controllers' update readiness and Fleet's verdict.
type Support struct {
	Eligible         bool
	IneligibleCode   string
	IneligibleReason string
	Instances        []sdk.FrigateInstanceSupport
}

// GetSupport reads each controller's readiness and decides whether an update
// may start now. It mutates nothing. When packageID is not Nil it also checks
// the package against the controllers.
func (s *Service) GetSupport(ctx context.Context, deviceIdentifier string, packageID uuid.UUID) (Support, error) {
	info, err := principal(ctx)
	if err != nil {
		return Support{}, err
	}
	target, err := s.target(ctx, info.OrganizationID, deviceIdentifier)
	if err != nil {
		return Support{}, err
	}
	var pkg *Package
	if packageID != uuid.Nil {
		row, err := s.queries.GetFrigateUpdatePackage(ctx, sqlc.GetFrigateUpdatePackageParams{OrgID: info.OrganizationID, ID: packageID})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Support{}, fleeterror.NewNotFoundError("package not found")
			}
			return Support{}, err
		}
		p := packageFromRow(row)
		if p.Deleted {
			return Support{}, fleeterror.NewNotFoundError("package not found")
		}
		pkg = &p
	}
	return s.readSupport(ctx, target.ID, pkg)
}

// target resolves and authorizes a paired CT1 container by its identifier.
func (s *Service) target(ctx context.Context, orgID int64, identifier string) (sqlc.GetFrigateUpdateTargetRow, error) {
	row, err := s.queries.GetFrigateUpdateTarget(ctx, sqlc.GetFrigateUpdateTargetParams{DeviceIdentifier: identifier, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.GetFrigateUpdateTargetRow{}, fleeterror.NewNotFoundError("container not found")
		}
		return sqlc.GetFrigateUpdateTargetRow{}, err
	}
	if err := s.auth.AuthorizeSite(ctx, siteArg(row.SiteID)); err != nil {
		return sqlc.GetFrigateUpdateTargetRow{}, err
	}
	return row, nil
}

// readSupport queries the controllers and evaluates eligibility. Controllers
// that cannot be reached make the container ineligible rather than failing the
// call, except when Fleet cannot update this kind of controller at all.
func (s *Service) readSupport(ctx context.Context, deviceID int64, pkg *Package) (Support, error) {
	updater, err := s.updaterFor(ctx, deviceID)
	if err != nil {
		return Support{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	defer cancel()
	instances, err := updater.FrigateUpdateSupport(callCtx)
	if err != nil {
		if fleeterror.IsFailedPreconditionError(err) || fleeterror.IsUnimplementedError(err) {
			return Support{}, err
		}
		code, reason := CodeControllerUnreachable, fmt.Sprintf("the controllers could not be read: %v", err)
		if refusal := sdk.AsFrigateRefusal(err); refusal != nil {
			code, reason = refusal.Reason, refusal.Message
		}
		return Support{IneligibleCode: code, IneligibleReason: reason}, nil
	}
	out := Support{Instances: instances}
	out.Eligible, out.IneligibleCode, out.IneligibleReason = evaluateSupport(instances, pkg)
	return out, nil
}

// evaluateSupport is Fleet's advisory eligibility verdict. Frigate remains the
// authority and re-checks every condition when the update starts.
func evaluateSupport(instances []sdk.FrigateInstanceSupport, pkg *Package) (eligible bool, code, reason string) {
	if len(instances) == 0 {
		return false, CodeNoInstances, "no controller reported its update status"
	}
	for _, in := range instances {
		if in.Err != "" {
			code := in.ErrReason
			if code == "" {
				code = CodeInstanceUnreachable
			}
			return false, code, fmt.Sprintf("controller %s could not be read: %s", in.InstanceID, in.Err)
		}
	}
	for _, in := range instances {
		if in.ActiveJobID != "" {
			return false, CodeUpdateInProgress, fmt.Sprintf("controller %s is already running update %s", in.InstanceID, in.ActiveJobID)
		}
	}
	for _, in := range instances {
		switch {
		case in.InstallationType != sdk.FrigateInstallationDebianPackage:
			return false, CodeUnsupportedInstallation, fmt.Sprintf("controller %s is not installed from a Debian package", in.InstanceID)
		case !in.HelperAvailable:
			return false, CodeHelperUnavailable, withDetail(fmt.Sprintf("the update helper is not available on controller %s", in.InstanceID), in.SupportMessage)
		case !in.SelfCoordinationSupported:
			return false, CodeSelfCoordination, fmt.Sprintf("controller %s does not support Fleet-coordinated updates; upgrade it first", in.InstanceID)
		case !in.RemoteTriggerAllowed:
			return false, CodeRemoteTriggerDisabled, fmt.Sprintf("controller %s does not allow remote update triggers; enable update.allow_remote_trigger on the controller", in.InstanceID)
		}
	}
	// The controllers refuse Start unless Fleet names each member of the HA
	// pair. Fleet knows only one of them when discovery found only one.
	for _, in := range instances {
		if in.PeerInstanceID != "" && !slices.ContainsFunc(instances, func(other sdk.FrigateInstanceSupport) bool {
			return other.InstanceID == in.PeerInstanceID
		}) {
			return false, CodeInstanceUnreachable, fmt.Sprintf("controller %s reports the HA peer %s, but Fleet does not know that controller", in.InstanceID, in.PeerInstanceID)
		}
	}
	if len(instances) > 1 {
		// The CT1 plugin refuses Start unless the controllers share one non-empty
		// etcd cluster ID (compared case-insensitively); check the same here so
		// the refusal surfaces before the package is staged.
		cluster := instances[0].ClusterID
		if cluster == "" {
			return false, CodeClusterMismatch, fmt.Sprintf("controller %s reports no HA cluster", instances[0].InstanceID)
		}
		for _, in := range instances[1:] {
			if !strings.EqualFold(in.ClusterID, cluster) {
				return false, CodeClusterMismatch, "the controllers report different HA clusters"
			}
		}
		for _, in := range instances {
			if in.EtcdMembersTotal > 0 && in.EtcdMembersHealthy*2 <= in.EtcdMembersTotal {
				return false, CodeEtcdUnhealthy, fmt.Sprintf("controller %s sees %d of %d etcd members healthy; the cluster has no quorum", in.InstanceID, in.EtcdMembersHealthy, in.EtcdMembersTotal)
			}
		}
	}
	if pkg == nil {
		return true, "", ""
	}
	for _, in := range instances {
		switch {
		case in.Architecture != "" && in.Architecture != pkg.Architecture:
			return false, CodeArchitectureMismatch, fmt.Sprintf("controller %s runs %s but the package is for %s", in.InstanceID, in.Architecture, pkg.Architecture)
		// A package that Fleet stored without verifying its signature has no
		// key ID to compare; the controllers verify the signature themselves.
		case pkg.ReleaseKeyID != "" && len(in.TrustedReleaseKeyIDs) > 0 && !slices.Contains(in.TrustedReleaseKeyIDs, pkg.ReleaseKeyID):
			return false, CodeReleaseKeyUntrusted, fmt.Sprintf("controller %s does not trust the key that signed this package (%s)", in.InstanceID, pkg.ReleaseKeyID)
		case in.SpoolFreeBytes < pkg.SizeBytes:
			return false, CodeInsufficientSpace, fmt.Sprintf("controller %s has %d bytes free for staging; the package needs %d", in.InstanceID, in.SpoolFreeBytes, pkg.SizeBytes)
		}
	}
	return true, "", ""
}

func withDetail(msg, detail string) string {
	if strings.TrimSpace(detail) == "" {
		return msg
	}
	return msg + ": " + detail
}

// instanceIDs returns the controllers' instance IDs, sorted. Frigate uses the
// sorted list as the rollout's expected set.
func instanceIDs(instances []sdk.FrigateInstanceSupport) []string {
	ids := make([]string, 0, len(instances))
	for _, in := range instances {
		ids = append(ids, in.InstanceID)
	}
	sort.Strings(ids)
	return slices.Compact(ids)
}
