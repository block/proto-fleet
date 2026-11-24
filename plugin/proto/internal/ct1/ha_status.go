package ct1

import (
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Frigate samples roles every five seconds and etcd every thirty seconds.
// Check original sample times as well as freshness flags; receipt is not refresh.
const (
	haRoleMaxAge = 15 * time.Second
	haEtcdMaxAge = 90 * time.Second
	haClockSkew  = 5 * time.Second
)

func currentHATime(observed *timestamppb.Timestamp, now time.Time, maxAge time.Duration) bool {
	if observed == nil || observed.CheckValid() != nil {
		return false
	}
	observedAt := observed.AsTime()
	return !observedAt.After(now.Add(haClockSkew)) && now.Sub(observedAt) <= maxAge
}

func freshHAObservation(observation *mdk.HAObservation, source string, now time.Time, maxAge time.Duration) bool {
	return observation.GetFreshness() == mdk.HAObservationFreshness_HA_OBSERVATION_FRESHNESS_FRESH &&
		observation.GetSourceServerId() == source && currentHATime(observation.GetObservedAt(), now, maxAge)
}

// Diagnostic state can make HA degraded. It never grants write authority.
func healthyHAReport(report *mdk.HAStatus, live *mdk.SystemStatus, endpoints []sdk.DeviceEndpoint, now time.Time) bool {
	if !report.GetEnabled() || !report.GetInventoryReady() ||
		report.GetIdentityState() != mdk.HAIdentityState_HA_IDENTITY_STATE_CONFIRMED ||
		len(report.GetConflicts()) != 0 {
		return false
	}
	// Membership affects HA health, not the durable container identity.
	if len(endpoints) != 2 || report.ReportingServerId == report.PeerServerId {
		return false
	}
	for _, endpoint := range endpoints {
		if endpoint.ID != report.ReportingServerId && endpoint.ID != report.PeerServerId {
			return false
		}
	}
	seen := map[string]bool{}
	primary, standby := 0, 0
	for _, member := range report.Members {
		switch member.GetKind() {
		case mdk.HAMemberRole_HA_MEMBER_ROLE_FRIGATE:
			isLocal := member.Id == report.ReportingServerId
			isPeer := member.Id == report.PeerServerId
			if seen[member.Id] || (!isLocal && !isPeer) || member.Local != isLocal {
				return false
			}
			seen[member.Id] = true
			if !freshHAObservation(member.RoleObservation, member.Id, now, haRoleMaxAge) ||
				!freshHAObservation(member.FrigateObservation, report.ReportingServerId, now, haRoleMaxAge) ||
				!member.GetFrigateReachable() || member.ContainerWritePrimary == nil ||
				member.GetRecovery().GetState() != mdk.HARecoveryState_HA_RECOVERY_STATE_STEADY {
				return false
			}
			if member.Local && (member.HaRole != live.GetHaRole() || member.GetContainerWritePrimary() != live.GetContainerWritePrimary()) {
				return false
			}
			if member.HaRole == mdk.HARole_HA_ROLE_PRIMARY && member.GetContainerWritePrimary() {
				primary++
			} else if member.HaRole == mdk.HARole_HA_ROLE_STANDBY && !member.GetContainerWritePrimary() {
				standby++
			} else {
				return false
			}
		case mdk.HAMemberRole_HA_MEMBER_ROLE_WITNESS:
			// Witnesses run etcd only. They have no Frigate role or write claim.
		case mdk.HAMemberRole_HA_MEMBER_ROLE_UNSPECIFIED:
			return false
		default:
			return false
		}
		if member.Kind == mdk.HAMemberRole_HA_MEMBER_ROLE_WITNESS || member.EtcdName != "" || member.EtcdObservation != nil || member.EtcdReachable != nil {
			if !member.GetEtcdReachable() || !freshHAObservation(member.EtcdObservation, report.ReportingServerId, now, haEtcdMaxAge) {
				return false
			}
		}
	}
	return primary == 1 && standby == 1
}
