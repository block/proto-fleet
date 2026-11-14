import type {
  ControllerDiagnostics,
  ControllerDiagnosticsResult,
  ControllerObservation,
  ControllerRecovery,
  ControllerRecoveryPhase,
  ControllerRole,
  ControllerStatusReport,
  HaIdentityState,
  HaReport,
} from "../controllerDiagnostics";
import {
  type V2GetHAStatusResponse,
  type V2GetSystemStatusResponse,
  V2HAIdentityState,
  V2HAMemberRole,
  type V2HAMemberStatus,
  type V2HAObservation,
  V2HAObservationFreshness,
  V2HARecoveryPhase,
  V2HARecoveryState,
  type V2HARecoveryStatus,
  V2HARole,
  type V2SystemStatus,
} from "./generated/generatedApi";

const text = (value: string | undefined) => value?.trim() || undefined;

// Unset protobuf timestamps can arrive as the Unix epoch; neither is an observation time.
const timeMs = (value: string | undefined) => {
  const ms = value ? Date.parse(value) : NaN;
  return Number.isFinite(ms) && ms > 0 ? ms : undefined;
};

const roles: Partial<Record<string, ControllerRole>> = {
  [V2HARole.HA_ROLE_PRIMARY]: "primary",
  [V2HARole.HA_ROLE_STANDBY]: "standby",
  [V2HARole.HA_ROLE_FENCED]: "fenced",
};

const identityStates: Partial<Record<string, HaIdentityState>> = {
  [V2HAIdentityState.HA_IDENTITY_STATE_CONFIRMED]: "confirmed",
  [V2HAIdentityState.HA_IDENTITY_STATE_CONFLICTED]: "conflicted",
  [V2HAIdentityState.HA_IDENTITY_STATE_UNAVAILABLE]: "unavailable",
};

const recoveryStates: Partial<Record<string, ControllerRecovery["state"]>> = {
  [V2HARecoveryState.HA_RECOVERY_STATE_STEADY]: "steady",
  [V2HARecoveryState.HA_RECOVERY_STATE_RECONCILING]: "reconciling",
  [V2HARecoveryState.HA_RECOVERY_STATE_DEGRADED]: "degraded",
};

const recoveryPhases: Partial<Record<string, ControllerRecoveryPhase>> = {
  [V2HARecoveryPhase.HA_RECOVERY_PHASE_STEADY]: "steady",
  [V2HARecoveryPhase.HA_RECOVERY_PHASE_DATABASE]: "database",
  [V2HARecoveryPhase.HA_RECOVERY_PHASE_CONTROL_STATE]: "controlState",
  [V2HARecoveryPhase.HA_RECOVERY_PHASE_ASSIGNMENTS]: "assignments",
  [V2HARecoveryPhase.HA_RECOVERY_PHASE_FLOTILLA_SYNC]: "flotillaSync",
};

const toObservation = (wire: V2HAObservation | undefined): ControllerObservation | undefined =>
  wire && {
    freshness:
      wire.freshness === V2HAObservationFreshness.HA_OBSERVATION_FRESHNESS_FRESH
        ? "fresh"
        : wire.freshness === V2HAObservationFreshness.HA_OBSERVATION_FRESHNESS_STALE
          ? "stale"
          : "unspecified",
    observedAtMs: timeMs(wire.observedAt),
    lastSuccessfulAtMs: timeMs(wire.lastSuccessfulAt),
    sourceId: text(wire.sourceServerId),
    detail: text(wire.detail),
  };

const toRecovery = (wire: V2HARecoveryStatus | undefined): ControllerRecovery | undefined =>
  wire && {
    state: recoveryStates[wire.state ?? ""] ?? "unknown",
    phase: recoveryPhases[wire.phase ?? ""] ?? "unknown",
    expectedBoards: wire.expectedBoards || undefined,
    reconciledBoards: wire.expectedBoards ? (wire.reconciledBoards ?? 0) : undefined,
    updatedAtMs: timeMs(wire.updatedAt),
    detail: text(wire.detail),
  };

const fromSystemStatus = (live: V2SystemStatus | undefined): ControllerDiagnostics => ({
  id: text(live?.serverId),
  local: true,
  reported: live !== undefined,
  version: text(live?.version),
  role: roles[live?.haRole ?? ""] ?? "unknown",
  writePrimary: live?.containerWritePrimary,
  recovery: toRecovery(live?.haRecovery),
});

const fromMember = (member: V2HAMemberStatus, local: boolean): ControllerDiagnostics => ({
  id: text(member.id),
  local,
  reported: true,
  hostname: text(member.hostname),
  role: roles[member.haRole ?? ""] ?? "unknown",
  writePrimary: member.containerWritePrimary,
  roleObservation: toObservation(member.roleObservation),
  reachable: member.frigateReachable,
  reachabilityObservation: toObservation(member.frigateObservation),
  recovery: toRecovery(member.recovery),
});

const errorMessage = (reason: unknown) =>
  text((reason as { message?: string } | undefined)?.message) ?? "The controller did not answer.";

// Older or single-controller deployments answer GetHAStatus as missing or unimplemented.
const isUnsupported = (reason: unknown) => {
  const status = (reason as { status?: number } | undefined)?.status;
  return status === 404 || status === 405 || status === 501;
};

const toHaReport = (result: PromiseSettledResult<V2GetHAStatusResponse>): HaReport => {
  if (result.status === "rejected") {
    return isUnsupported(result.reason)
      ? { status: "unsupported" }
      : { status: "unavailable", message: errorMessage(result.reason) };
  }
  const wire = result.value.status;
  if (!wire) return { status: "unavailable", message: "The controller returned an empty HA report." };
  return {
    status: "reported",
    enabled: wire.enabled ?? false,
    clusterId: text(wire.clusterId),
    identityState: identityStates[wire.identityState ?? ""] ?? "unknown",
    inventoryReady: wire.inventoryReady ?? false,
    observedAtMs: timeMs(wire.observedAt),
    conflicts: (wire.conflicts ?? []).map((conflict) => conflict.trim()).filter(Boolean),
    detail: text(wire.detail),
  };
};

const toStatusReport = (result: PromiseSettledResult<V2GetSystemStatusResponse>): ControllerStatusReport =>
  result.status === "rejected"
    ? { status: "unavailable", message: errorMessage(result.reason) }
    : {
        status: "reported",
        databaseConnected: result.value.status?.databaseConnected ?? false,
        dcsHealthy: result.value.status?.dcsHealthy,
      };

/**
 * Joins this controller's HA report with its live system status. Neither
 * source is required: a failed or unsupported HA read still yields the local
 * controller and an unreported peer, so one unreachable peer or an older
 * controller never hides the diagnostics that are available.
 */
export const toCanonicalControllerDiagnostics = (
  haResult: PromiseSettledResult<V2GetHAStatusResponse>,
  statusResult: PromiseSettledResult<V2GetSystemStatusResponse>,
): ControllerDiagnosticsResult => {
  const ha = toHaReport(haResult);
  const status = toStatusReport(statusResult);
  const live = statusResult.status === "fulfilled" ? statusResult.value.status : undefined;
  const liveController = fromSystemStatus(live);

  if (ha.status !== "reported" || haResult.status !== "fulfilled") {
    return { ha, status, controllers: [liveController, { local: false, reported: false, role: "unknown" }] };
  }
  if (!ha.enabled) return { ha, status, controllers: [liveController] };

  const wire = haResult.value.status!;
  const reportingId = text(wire.reportingServerId) ?? liveController.id;
  const members = (wire.members ?? []).filter((member) => member.kind === V2HAMemberRole.HA_MEMBER_ROLE_FRIGATE);
  const ids = [reportingId, text(wire.peerServerId), ...members.map((member) => text(member.id))].filter(
    (id, index, all): id is string => id !== undefined && all.indexOf(id) === index,
  );
  const controllers = ids.map((id): ControllerDiagnostics => {
    const local = id === reportingId;
    const member = members.find((candidate) => text(candidate.id) === id);
    if (!member) return local ? { ...liveController, id } : { id, local, reported: false, role: "unknown" };
    const controller = fromMember(member, local);
    // Only the answering controller reports its own version and live recovery.
    return local && (liveController.id === undefined || liveController.id === id)
      ? { ...controller, version: liveController.version, recovery: controller.recovery ?? liveController.recovery }
      : controller;
  });
  return { ha, status, controllers: controllers.sort((a, b) => Number(b.local) - Number(a.local)) };
};
