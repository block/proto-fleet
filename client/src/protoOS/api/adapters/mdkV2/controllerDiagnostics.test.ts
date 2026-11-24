import { describe, expect, test, vi } from "vitest";

import { toCanonicalControllerDiagnostics } from "./controllerDiagnostics";
import {
  type V2GetHAStatusResponse,
  type V2GetSystemStatusResponse,
  V2HAIdentityState,
  V2HAMemberRole,
  V2HAObservationFreshness,
  V2HARecoveryPhase,
  V2HARecoveryState,
  V2HARole,
} from "./generated/generatedApi";
import { CONTROLLER_DIAGNOSTICS_TIMEOUT_MS, MdkV2Adapter, type MdkV2Client } from "./MdkV2Adapter";

const fulfilled = <T>(value: T): PromiseSettledResult<T> => ({ status: "fulfilled", value });
const rejected = (reason: unknown): PromiseSettledResult<never> => ({ status: "rejected", reason });

const observedAt = "2026-11-13T10:00:00Z";
const observedAtMs = Date.parse(observedAt);

const liveStatus: V2GetSystemStatusResponse = {
  status: {
    serverId: "ct1-a",
    version: "4.2.0",
    haRole: V2HARole.HA_ROLE_PRIMARY,
    containerWritePrimary: true,
    databaseConnected: true,
    dcsHealthy: true,
    haRecovery: { state: V2HARecoveryState.HA_RECOVERY_STATE_STEADY },
  },
};

const haReport = (members: NonNullable<V2GetHAStatusResponse["status"]>["members"]): V2GetHAStatusResponse => ({
  status: {
    enabled: true,
    reportingServerId: "ct1-a",
    peerServerId: "ct1-b",
    clusterId: "cluster-1",
    identityState: V2HAIdentityState.HA_IDENTITY_STATE_CONFIRMED,
    inventoryReady: true,
    observedAt,
    members,
    conflicts: [],
  },
});

const localMember = {
  id: "ct1-a",
  kind: V2HAMemberRole.HA_MEMBER_ROLE_FRIGATE,
  local: true,
  hostname: "controller-a",
  haRole: V2HARole.HA_ROLE_PRIMARY,
  containerWritePrimary: true,
  frigateReachable: true,
  roleObservation: {
    sourceServerId: "ct1-a",
    observedAt,
    freshness: V2HAObservationFreshness.HA_OBSERVATION_FRESHNESS_FRESH,
  },
  frigateObservation: {
    sourceServerId: "ct1-a",
    observedAt,
    freshness: V2HAObservationFreshness.HA_OBSERVATION_FRESHNESS_FRESH,
  },
};

describe("toCanonicalControllerDiagnostics", () => {
  test("reports both controllers with independently aged observations and the local version", () => {
    const result = toCanonicalControllerDiagnostics(
      fulfilled(
        haReport([
          {
            id: "ct1-b",
            kind: V2HAMemberRole.HA_MEMBER_ROLE_FRIGATE,
            local: false,
            haRole: V2HARole.HA_ROLE_STANDBY,
            containerWritePrimary: false,
            frigateReachable: false,
            recovery: {
              state: V2HARecoveryState.HA_RECOVERY_STATE_RECONCILING,
              phase: V2HARecoveryPhase.HA_RECOVERY_PHASE_ASSIGNMENTS,
              expectedBoards: 288,
              reconciledBoards: 120,
            },
            roleObservation: {
              sourceServerId: "ct1-b",
              observedAt: "2026-11-13T09:55:00Z",
              freshness: V2HAObservationFreshness.HA_OBSERVATION_FRESHNESS_STALE,
            },
            frigateObservation: {
              sourceServerId: "ct1-a",
              observedAt,
              lastSuccessfulAt: "2026-11-13T09:55:00Z",
              freshness: V2HAObservationFreshness.HA_OBSERVATION_FRESHNESS_STALE,
              detail: "peer did not answer",
            },
          },
          // Witnesses are etcd-only voters, not controllers.
          { id: "witness", kind: V2HAMemberRole.HA_MEMBER_ROLE_WITNESS },
          localMember,
        ]),
      ),
      fulfilled(liveStatus),
    );

    expect(result.ha).toMatchObject({ status: "reported", enabled: true, identityState: "confirmed", observedAtMs });
    expect(result.status).toEqual({ status: "reported", databaseConnected: true, dcsHealthy: true });
    expect(result.controllers).toEqual([
      expect.objectContaining({
        id: "ct1-a",
        local: true,
        reported: true,
        hostname: "controller-a",
        version: "4.2.0",
        role: "primary",
        writePrimary: true,
        reachable: true,
        reachabilityObservation: expect.objectContaining({ freshness: "fresh", observedAtMs }),
        recovery: expect.objectContaining({ state: "steady" }),
      }),
      {
        id: "ct1-b",
        local: false,
        reported: true,
        hostname: undefined,
        role: "standby",
        writePrimary: false,
        reachable: false,
        roleObservation: {
          freshness: "stale",
          observedAtMs: Date.parse("2026-11-13T09:55:00Z"),
          lastSuccessfulAtMs: undefined,
          sourceId: "ct1-b",
          detail: undefined,
        },
        reachabilityObservation: {
          freshness: "stale",
          observedAtMs,
          lastSuccessfulAtMs: Date.parse("2026-11-13T09:55:00Z"),
          sourceId: "ct1-a",
          detail: "peer did not answer",
        },
        recovery: {
          state: "reconciling",
          phase: "assignments",
          expectedBoards: 288,
          reconciledBoards: 120,
          updatedAtMs: undefined,
          detail: undefined,
        },
      },
    ]);
  });

  test("keeps a configured peer the report has no observations for, without inventing health", () => {
    const result = toCanonicalControllerDiagnostics(fulfilled(haReport([localMember])), fulfilled(liveStatus));
    expect(result.controllers[1]).toEqual({ id: "ct1-b", local: false, reported: false, role: "unknown" });
  });

  test("treats unknown enum values, epoch timestamps and missing reachability as unreported", () => {
    const result = toCanonicalControllerDiagnostics(
      fulfilled(
        haReport([
          {
            ...localMember,
            haRole: "HA_ROLE_9" as V2HARole,
            frigateReachable: undefined,
            frigateObservation: { observedAt: "1970-01-01T00:00:00Z" },
          },
        ]),
      ),
      rejected(Object.assign(new Error("unavailable"), { status: 503 })),
    );
    expect(result.status).toEqual({ status: "unavailable", message: "unavailable" });
    expect(result.controllers[0]).toMatchObject({
      role: "unknown",
      reachable: undefined,
      version: undefined,
      reachabilityObservation: { freshness: "unspecified", observedAtMs: undefined },
    });
  });

  test("shows the local controller and an unreported peer when HA status is unsupported", () => {
    const result = toCanonicalControllerDiagnostics(
      rejected(Object.assign(new Error("not implemented"), { status: 501 })),
      fulfilled(liveStatus),
    );
    expect(result.ha).toEqual({ status: "unsupported" });
    expect(result.controllers).toEqual([
      expect.objectContaining({ id: "ct1-a", local: true, reported: true, version: "4.2.0", role: "primary" }),
      { local: false, reported: false, role: "unknown" },
    ]);
  });

  test("distinguishes an HA read failure from an unsupported controller", () => {
    const result = toCanonicalControllerDiagnostics(
      rejected(Object.assign(new Error("deadline exceeded"), { status: 504 })),
      fulfilled(liveStatus),
    );
    expect(result.ha).toEqual({ status: "unavailable", message: "deadline exceeded" });
    expect(result.controllers).toHaveLength(2);
  });

  test("shows only the local controller when HA is not enabled", () => {
    const result = toCanonicalControllerDiagnostics(
      fulfilled({ status: { enabled: false, reportingServerId: "ct1-a" } }),
      fulfilled(liveStatus),
    );
    expect(result.ha).toMatchObject({ status: "reported", enabled: false });
    expect(result.controllers.map((controller) => controller.id)).toEqual(["ct1-a"]);
  });
});

describe("MdkV2Adapter.getControllerDiagnostics", () => {
  test("reads HA and system status with the actor header", async () => {
    const getHa = vi.fn().mockResolvedValue({ data: haReport([localMember]) });
    const getStatus = vi.fn().mockResolvedValue({ data: liveStatus });
    const adapter = new MdkV2Adapter({
      systemServiceGetHaStatus: getHa,
      systemServiceGetSystemStatus: getStatus,
    } as unknown as MdkV2Client);

    const result = await adapter.getControllerDiagnostics();

    const actor = expect.objectContaining({ headers: { "X-Frigate-Actor": "proto-os" } });
    expect(getHa).toHaveBeenCalledWith(actor);
    expect(getStatus).toHaveBeenCalledWith(actor);
    expect(result.controllers.map((controller) => controller.id)).toEqual(["ct1-a", "ct1-b"]);
  });

  test("fails only when neither source answers", async () => {
    const failure = Object.assign(new Error("offline"), { status: 503 });
    const adapter = new MdkV2Adapter({
      systemServiceGetHaStatus: vi.fn().mockRejectedValue(failure),
      systemServiceGetSystemStatus: vi.fn().mockRejectedValue(failure),
    } as unknown as MdkV2Client);

    await expect(adapter.getControllerDiagnostics()).rejects.toBe(failure);
  });

  test("keeps the system status when HA status is unsupported", async () => {
    const adapter = new MdkV2Adapter({
      systemServiceGetHaStatus: vi.fn().mockRejectedValue(Object.assign(new Error("unimplemented"), { status: 501 })),
      systemServiceGetSystemStatus: vi.fn().mockResolvedValue({ data: liveStatus }),
    } as unknown as MdkV2Client);

    const result = await adapter.getControllerDiagnostics();

    expect(result.ha).toEqual({ status: "unsupported" });
    expect(result.status).toMatchObject({ status: "reported", databaseConnected: true });
    expect(result.controllers[0]).toMatchObject({ id: "ct1-a", local: true, version: "4.2.0" });
  });

  test("keeps the HA report when system status fails", async () => {
    const adapter = new MdkV2Adapter({
      systemServiceGetHaStatus: vi.fn().mockResolvedValue({ data: haReport([localMember]) }),
      systemServiceGetSystemStatus: vi.fn().mockRejectedValue(Object.assign(new Error("offline"), { status: 503 })),
    } as unknown as MdkV2Client);

    const result = await adapter.getControllerDiagnostics();

    expect(result.status).toEqual({ status: "unavailable", message: "offline" });
    expect(result.ha).toMatchObject({ status: "reported", enabled: true });
    expect(result.controllers.map((controller) => controller.id)).toEqual(["ct1-a", "ct1-b"]);
  });

  test("reports a stalled HA read as unavailable without holding back system status", async () => {
    vi.useFakeTimers();
    try {
      // Like the real transports, the stalled read settles only when its signal aborts.
      const stalled = vi.fn(
        ({ signal }: { signal: AbortSignal }) =>
          new Promise((_, reject) => signal.addEventListener("abort", () => reject(signal.reason))),
      );
      const adapter = new MdkV2Adapter({
        systemServiceGetHaStatus: stalled,
        systemServiceGetSystemStatus: vi.fn().mockResolvedValue({ data: liveStatus }),
      } as unknown as MdkV2Client);

      const pending = adapter.getControllerDiagnostics();
      await vi.advanceTimersByTimeAsync(CONTROLLER_DIAGNOSTICS_TIMEOUT_MS);
      const result = await pending;

      expect(result.ha).toEqual({ status: "unavailable", message: "The controller did not respond within 8 seconds." });
      expect(result.controllers[0]).toMatchObject({ id: "ct1-a", version: "4.2.0" });
    } finally {
      vi.useRealTimers();
    }
  });
});
