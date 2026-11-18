import { Code } from "@connectrpc/connect";
import {
  type FeedMapping,
  type MappingAssessment,
  type MappingBoard,
  mappingError,
  type MappingRun,
} from "../feedMapping";
import type { AdapterRequestOptions } from "../types";
import type {
  V2FeedMappingRun,
  V2FeedMappingRunConfig,
  V2GetContainerMapResponse,
  V2GetFeedMappingReportResponse,
  V2GetFeedMappingRunResponse,
  V2RequestWiringVerificationResponse,
} from "./generated/generatedApi";
import { moduleFeedMapping } from "./modulePowerFeed";
import { containerWriteError } from "./writeErrors";

type Response<T> = Promise<{ data: T }>;
export interface FeedMappingClient {
  report(params?: AdapterRequestOptions): Response<V2GetFeedMappingReportResponse>;
  active(params?: AdapterRequestOptions): Response<V2GetFeedMappingRunResponse>;
  get(id: string, params?: AdapterRequestOptions): Response<V2GetFeedMappingRunResponse>;
  plan(config: V2FeedMappingRunConfig, params?: AdapterRequestOptions): Response<{ plan?: V2FeedMappingRun }>;
  start(config: V2FeedMappingRunConfig, params?: AdapterRequestOptions): Response<V2GetFeedMappingRunResponse>;
  cancel(id: string, params?: AdapterRequestOptions): Response<V2GetFeedMappingRunResponse>;
  verify(macs: string[], params?: AdapterRequestOptions): Response<V2RequestWiringVerificationResponse>;
}

const text = (value?: string) => value?.trim() || undefined;
const conflictReasons = new Set([
  "multiple_feeds",
  "feed_shared",
  "configured_location_mismatch",
  "module_identity_changed",
  "duplicate_module",
  "firmware_position_mismatch",
  "psu_group_location_conflict",
  "duplicate_slot_occupancy",
  "conflicting_switch_observations",
  "legacy_location_disagrees_with_schedule",
  "legacy_outlet_disagrees_with_schedule",
  "switch_identity_mismatch",
]);

/** Assess actual board records, including the 0/0 inventory that module feed inspection omits. */
export function assessMapping(
  map: V2GetContainerMapResponse,
  report?: V2GetFeedMappingReportResponse,
  unavailable?: string,
): MappingAssessment {
  const boards = new Map<string, MappingBoard>();
  for (const tank of map.containerMap?.tanks ?? []) {
    for (const module of tank.modules ?? []) {
      const moduleReasons =
        tank.id && module.id
          ? moduleFeedMapping(map, {
              tank: tank.id,
              module: module.id,
              reportedId: module.slots?.find((s) => s.hashboard?.moduleId)?.hashboard?.moduleId,
            }).reasons.filter((r) => conflictReasons.has(r))
          : [];
      for (const slot of module.slots ?? []) {
        const board = slot.hashboard;
        const mac = text(board?.macAddress)?.toLowerCase();
        if (!mac || !board || slot.occupied === false) continue;
        const location = board.location;
        const reasons = [...new Set([...moduleReasons, ...(location?.wiringReasons ?? [])])];
        if (!text(location?.powerFeedId) && !text(slot.powerFeedId) && !text(location?.expectedPowerFeedId))
          reasons.push("no_feed_assigned");
        const previous = boards.get(mac);
        if (previous && (previous.tank !== tank.id || previous.module !== module.id))
          reasons.push("duplicate_board_location");
        const status = location?.wiringStatus?.toLowerCase() || "unverified";
        boards.set(mac, {
          mac,
          moduleId: text(board.moduleId),
          tank: tank.id,
          module: module.id,
          position: location?.hashboardPosition,
          expectedFeed: text(location?.expectedPowerFeedId),
          assignedFeed: text(location?.powerFeedId) ?? text(slot.powerFeedId),
          status,
          reasons: [...new Set([...(previous?.reasons ?? []), ...reasons])],
          coverage: [],
          unlocated: !(tank.id && tank.id > 0 && module.id && module.id > 0),
          conflict:
            previous?.conflict === true ||
            status === "mismatch" ||
            reasons.some((r) => conflictReasons.has(r) || r === "duplicate_board_location"),
        });
      }
    }
  }
  for (const wire of report?.wiring?.boards ?? []) {
    const mac = text(wire.macAddress)?.toLowerCase();
    if (!mac || wire.supersededBy) continue;
    // Historical wiring records do not establish current occupancy.
    const board = boards.get(mac);
    if (!board) continue;
    board.expectedFeed = text(wire.expectedFeedId) ?? board.expectedFeed;
    board.observedFeed = text(wire.observedFeedId);
    board.switchPort = [wire.switchId, wire.portId].filter(Boolean).join(" / ") || undefined;
    if (wire.tank && wire.module)
      board.expectedLocation = `Tank ${wire.tank}, module ${wire.module}${wire.hashboardPosition ? `, HB${wire.hashboardPosition}` : ""}`;
    board.status = wire.status?.toLowerCase() || board.status;
    board.reasons = [
      ...new Set([...board.reasons, ...(wire.reasons ?? []), ...(wire.deferredReason ? [wire.deferredReason] : [])]),
    ];
    board.coverage = wire.reducedCoverage ?? [];
    board.verifiedAt = wire.verifiedAt;
    board.conflict ||= board.status === "mismatch" || board.reasons.some((r) => conflictReasons.has(r));
  }
  return {
    boards: [...boards.values()],
    mode: report ? (report.wiring ? report.wiring.mode || "unknown" : "manual") : "unknown",
    hardMappingAvailable: report?.report !== undefined,
    unavailable: unavailable ?? (!map.containerMap ? "Container topology is unavailable." : undefined),
    configurationReasons: report?.wiring?.configurationReasons ?? [],
    deferredReason: report?.wiring?.deferredReason,
    changes: (report?.wiring?.history ?? [])
      .slice()
      .reverse()
      .map((event) => ({
        at: event.at,
        mac: event.macAddress ?? "",
        detail: event.detail || event.kind || "Mapping changed",
      }))
      .concat(
        (report?.changes ?? []).map((change) => ({
          at: change.changedAt,
          mac: change.macAddress ?? "",
          detail: `${change.previousFeedId || "Unassigned"} → ${change.feedId || "Unassigned"}`,
        })),
      )
      .sort((a, b) => (b.at ?? "").localeCompare(a.at ?? "")),
    historyIncomplete: report?.changesTruncated === true || report?.changesUnavailable === true,
  };
}

export function mappingRun(run?: V2FeedMappingRun): MappingRun {
  if (!run?.runId) throw new Error("The controller did not return a mapping run.");
  const status = run.status?.replace("FEED_MAPPING_RUN_STATUS_", "").toLowerCase();
  return {
    id: run.runId,
    status:
      status === "planned" ||
      status === "running" ||
      status === "completed" ||
      status === "failed" ||
      status === "canceled"
        ? status
        : "unknown",
    phase: run.phase ?? "",
    currentFeed: text(run.currentFeedId),
    error: text(run.error),
    warnings: run.warnings ?? [],
    feeds: (run.feeds ?? []).map((f) => ({
      id: f.feedId ?? "",
      boards: f.boards?.length ?? 0,
      complete: f.status === "FEED_MAPPING_FEED_STATUS_COMPLETED" || f.status === "FEED_MAPPING_FEED_STATUS_TIMED_OUT",
      timedOut: f.timedOut === true || f.offlineTimedOut === true,
    })),
  };
}

export function createFeedMapping(
  client: FeedMappingClient,
  readMap: (params?: AdapterRequestOptions) => Response<V2GetContainerMapResponse>,
  decorate: (params?: AdapterRequestOptions) => AdapterRequestOptions,
): FeedMapping {
  const readRun = async (request: () => Response<V2GetFeedMappingRunResponse>): Promise<MappingRun | null> => {
    try {
      return mappingRun((await request()).data.run);
    } catch (error) {
      if ((error as { error?: { code?: number } })?.error?.code === Code.NotFound) return null;
      throw error;
    }
  };
  const write = async <T>(request: () => Promise<T>): Promise<T> => {
    try {
      return await request();
    } catch (error) {
      throw containerWriteError(
        error,
        "No mapping action started.",
        "The mapping request outcome is unknown. Check the controller before you try again.",
        new Set([
          Code.InvalidArgument,
          Code.FailedPrecondition,
          Code.Unimplemented,
          Code.Aborted,
          Code.PermissionDenied,
          Code.Unauthenticated,
        ]),
      );
    }
  };
  return {
    async assess(params) {
      const [map, report] = await Promise.allSettled([readMap(decorate(params)), client.report(decorate(params))]);
      if (map.status === "rejected") throw map.reason;
      return assessMapping(
        map.value.data,
        report.status === "fulfilled" ? report.value.data : undefined,
        report.status === "rejected" ? mappingError(report.reason) : undefined,
      );
    },
    active: (params) => readRun(() => client.active(decorate(params))),
    get: (id, params) => readRun(() => client.get(id, decorate(params))),
    async plan(params) {
      return mappingRun((await client.plan({ dryRun: true, restoreAllOn: true }, decorate(params))).data.plan);
    },
    start: (feedIds, params) =>
      write(async () => {
        if (!feedIds.length) throw new Error("No power feeds were selected.");
        return mappingRun(
          (
            await client.start(
              { feedIds, dryRun: false, restoreAllOn: true, confirmPowerCycle: true },
              decorate(params),
            )
          ).data.run,
        );
      }),
    cancel: (id, params) => write(async () => mappingRun((await client.cancel(id, decorate(params))).data.run)),
    verify: (macs, params) =>
      write(async () => {
        if (!macs.length) throw new Error("No hashboards were selected.");
        if (!(await client.verify(macs, decorate(params))).data.queued)
          throw new Error("The controller did not confirm verification.");
      }),
  };
}
