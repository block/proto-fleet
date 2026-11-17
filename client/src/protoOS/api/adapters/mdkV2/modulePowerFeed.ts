import type { ModulePowerFeedInspection } from "../types";
import {
  type V2GetActivePowerRecoveryCycleResponse,
  type V2GetContainerMapResponse,
  type V2HashboardLocation,
  type V2ListPowerFeedsResponse,
  type V2PowerFeed,
  V2PowerFeedState,
  type V2Slot,
} from "./generated/generatedApi";
import type {
  PowerFeedConfidence,
  PowerFeedModuleRef,
  PowerFeedReadingView,
  PowerRecoveryCycleView,
} from "@/shared/components/PowerFeedCard";

// These rules mirror the Fleet CT1 plugin so a module reads the same mapping
// confidence in ProtoOS and in Fleet. A change to any of these needs the same
// change here:
// - plugin/proto/internal/ct1/power_feed.go: moduleFeedMapping and its reasons
// - plugin/proto/internal/ct1/protobuf.go: slot placement and slot validity
// - plugin/proto/internal/ct1/hashboards.go: collectTopologyIdentities, uniqueModule
// - server/sdk/v1/hashboard_validation.go: ValidateHashboardJSON

const BOARDS_PER_MODULE = 3;

/** A module position and the module ID the operator saw there, if the controller reported one. */
export type ModuleFeedTarget = { tank: number; module: number; reportedId?: string };

type SlotEvidence = {
  occupied: boolean;
  /** No slot record named this position. */
  unreported: boolean;
  invalid: boolean;
  /** The board's module ID as reported, kept only when it is not blank. */
  moduleId?: string;
  feed?: { ids: string[]; wiringStatus: string; configuredTank?: number; configuredModule?: number };
};

const trim = (value: string | undefined) => value?.trim() ?? "";

const feedIds = (slot: V2Slot) => {
  const location: V2HashboardLocation | undefined = slot.hashboard?.location;
  const ids: string[] = [];
  for (const id of [slot.powerFeedId, location?.powerFeedId, location?.expectedPowerFeedId].map(trim)) {
    if (id && !ids.includes(id)) ids.push(id);
  }
  return ids;
};

const LONE_SURROGATE = /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/;
/** Like the plugin's board validation: a payload holding a NUL or an unpaired surrogate is not trusted. */
const unstorable = (value: unknown): boolean =>
  typeof value === "string"
    ? value.includes("\0") || LONE_SURROGATE.test(value)
    : !!value && typeof value === "object" && Object.entries(value).some(([k, v]) => unstorable(k) || unstorable(v));

/** Places each reported slot at its board position, keeping extra or unplaceable claims. */
function moduleSlots(slots: V2Slot[] | undefined): SlotEvidence[] {
  const placed: SlotEvidence[] = Array.from({ length: BOARDS_PER_MODULE }, () => ({
    occupied: false,
    unreported: true,
    invalid: false,
  }));
  const extra: SlotEvidence[] = [];
  for (const slot of slots ?? []) {
    const location = slot.hashboard?.location;
    const evidence: SlotEvidence = {
      occupied: slot.occupied === true,
      unreported: false,
      invalid: unstorable(slot.hashboard),
      moduleId: trim(slot.hashboard?.moduleId) ? slot.hashboard?.moduleId : undefined,
      feed: {
        ids: feedIds(slot),
        wiringStatus: trim(location?.wiringStatus).toLowerCase(),
        configuredTank: slot.configuredLocation?.tank || undefined,
        configuredModule: slot.configuredLocation?.module || undefined,
      },
    };
    // Tolerates positions serialized as strings.
    const configured = Number(slot.configuredLocation?.hashboardPosition ?? 0) || 0;
    const position = Number(location?.hashboardPosition ?? 0) || configured;
    if (position < 1 || position > BOARDS_PER_MODULE) {
      extra.push({ ...evidence, invalid: true });
      continue;
    }
    if (configured !== 0 && configured !== position) evidence.invalid = true;
    if (placed[position - 1].unreported) placed[position - 1] = evidence;
    else extra.push(evidence);
  }
  return [...placed, ...extra];
}

type Placement = { tank: number; module: number; slots: SlotEvidence[] };

function placements(map: V2GetContainerMapResponse): Placement[] {
  return (map.containerMap?.tanks ?? []).flatMap((tank) =>
    (tank.modules ?? []).flatMap((module) =>
      // Unlocated inventory has no physical slots or feed assignment.
      tank.id === undefined || module.id === undefined || (tank.id === 0 && module.id === 0)
        ? []
        : [{ tank: tank.id, module: module.id, slots: moduleSlots(module.slots) }],
    ),
  );
}

/**
 * A reported module ID must name exactly this position, and the position no
 * other ID. Like the plugin, positions are matched on the trimmed ID, while a
 * position's claims count IDs as reported, so a padded ID is a second claim.
 */
function identityUnique(all: Placement[], target: ModuleFeedTarget, reportedId: string) {
  const locations = new Set<string>();
  const claims = new Set<string>();
  for (const placement of all) {
    if (placement.tank === 0) continue;
    const key = `${placement.tank}:${placement.module}`;
    for (const slot of placement.slots) {
      if (!slot.moduleId) continue;
      if (slot.moduleId.trim() === reportedId) locations.add(key);
      if (key === `${target.tank}:${target.module}`) claims.add(slot.moduleId);
    }
  }
  return locations.size === 1 && locations.has(`${target.tank}:${target.module}`) && claims.size === 1;
}

const wiringReasonSuffix = (status: string) => status.replace(/[^a-z0-9_]/g, "_").slice(0, 32);
const benignWiringStatus = (status: string) => status === "" || status === "verified" || status === "unverified";

export type ModuleFeedMapping = {
  feedId?: string;
  confidence: PowerFeedConfidence;
  reasons: string[];
  sharedWith: PowerFeedModuleRef[];
};

/**
 * Derives a module's feed from every slot's evidence: the slot's configured
 * feed, the board's observed and expected feeds, and its wiring status. A
 * unique mapping needs exactly one feed across all of it, claimed by no other
 * module, at a module identity the controller reports uniquely.
 */
export function moduleFeedMapping(map: V2GetContainerMapResponse, target: ModuleFeedTarget): ModuleFeedMapping {
  const unavailable = (reason: string): ModuleFeedMapping => ({
    confidence: "unavailable",
    reasons: [reason],
    sharedWith: [],
  });
  if (!map.containerMap) return unavailable("topology_unavailable");
  const all = placements(map);
  const matches = all.filter((p) => p.tank === target.tank && p.module === target.module);
  if (!matches.length) return unavailable("module_not_in_topology");
  const reasons = new Set<string>();
  if (matches.length > 1) reasons.add("duplicate_module");
  const reportedId = trim(target.reportedId);
  if (!reportedId) reasons.add("module_identity_unconfirmed");
  else if (!identityUnique(all, target, reportedId)) reasons.add("module_identity_changed");

  const ids: string[] = [];
  let occupied = 0;
  let verified = 0;
  let unnamed = false;
  // Like the plugin, duplicate entries for one position read the last one.
  for (const slot of matches[matches.length - 1].slots) {
    if (slot.invalid) reasons.add("slot_placement_invalid");
    const feed = slot.feed;
    if (feed) {
      for (const id of feed.ids) if (!ids.includes(id)) ids.push(id);
      if (!benignWiringStatus(feed.wiringStatus)) reasons.add(`wiring_${wiringReasonSuffix(feed.wiringStatus)}`);
      if (
        (feed.configuredTank !== undefined && feed.configuredTank !== target.tank) ||
        (feed.configuredModule !== undefined && feed.configuredModule !== target.module)
      )
        reasons.add("configured_location_mismatch");
    }
    if (slot.occupied) {
      if (!feed?.ids.length) unnamed = true;
      occupied++;
      if (feed?.wiringStatus === "verified") verified++;
    }
  }

  let feedId: string | undefined;
  let sharedWith: PowerFeedModuleRef[] = [];
  if (ids.length === 0) reasons.add("no_feed_assigned");
  else if (ids.length > 1) reasons.add("multiple_feeds");
  else {
    feedId = ids[0];
    sharedWith = all
      .filter(
        (p) =>
          p.tank !== 0 &&
          !(p.tank === target.tank && p.module === target.module) &&
          p.slots.some((slot) => slot.feed?.ids.includes(ids[0])),
      )
      .map(({ tank, module }) => ({ tank, module }));
    if (sharedWith.length) reasons.add("feed_shared");
  }
  if (unnamed && ids.length) reasons.add("feed_evidence_incomplete");

  const sorted = [...reasons].sort();
  const confidence: PowerFeedConfidence =
    ids.length === 0 && reasons.size === 1
      ? "unavailable"
      : reasons.size
        ? "ambiguous"
        : occupied > 0 && verified === occupied
          ? "wiring_verified"
          : "configured";
  return { feedId, confidence, reasons: sorted, sharedWith };
}

const finite = (value: number | undefined) => (value === undefined || !Number.isFinite(value) ? undefined : value);

export function powerFeedReading(feed: V2PowerFeed): PowerFeedReadingView {
  return {
    state:
      feed.state === V2PowerFeedState.POWER_FEED_STATE_ENABLED
        ? "enabled"
        : feed.state === V2PowerFeedState.POWER_FEED_STATE_DISABLED
          ? "disabled"
          : "unknown",
    active: feed.active,
    faulted: feed.faulted,
    overridden: feed.overridden === true,
    voltageV: finite(feed.voltageV),
    currentA: finite(feed.currentA),
    powerW: finite(feed.powerW),
    lineVoltagesV: [finite(feed.voltageAbV), finite(feed.voltageBcV), finite(feed.voltageCaV)],
    phaseCurrentsA: [finite(feed.currentAA), finite(feed.currentBA), finite(feed.currentCA)],
  };
}

/** The current reading of exactly one feed, or why there is none. */
export function feedReading(
  response: V2ListPowerFeedsResponse,
  id: string,
): { reading: PowerFeedReadingView; reason?: undefined } | { reading?: undefined; reason: string } {
  if (response.snapshotReady === false) return { reason: "snapshot_not_ready" };
  const matches = (response.powerFeeds ?? []).filter((feed) => trim(feed.powerFeedId) === id);
  if (matches.length > 1) return { reason: "duplicate_records" };
  if (!matches.length) return { reason: "not_in_snapshot" };
  return { reading: powerFeedReading(matches[0]) };
}

export function recoveryCycle(response: V2GetActivePowerRecoveryCycleResponse): PowerRecoveryCycleView | null {
  const cycle = response.cycle;
  if (!cycle || !trim(cycle.id)) return null;
  const restoreAfter = cycle.restoreAfter ? new Date(cycle.restoreAfter) : undefined;
  return {
    phase: cycle.phase ?? "",
    outletIds: cycle.outletIds ?? [],
    restoreAfter: restoreAfter && !Number.isNaN(restoreAfter.getTime()) ? restoreAfter : undefined,
    lastError: cycle.lastError ?? "",
  };
}

/** One inspection from reads taken together; only a failed map read fails the inspection. */
export function modulePowerFeedInspection(
  target: ModuleFeedTarget,
  reads: {
    map: V2GetContainerMapResponse;
    feeds: PromiseSettledResult<V2ListPowerFeedsResponse>;
    cycle: PromiseSettledResult<V2GetActivePowerRecoveryCycleResponse>;
  },
  observedAt: Date,
): ModulePowerFeedInspection {
  const mapping = moduleFeedMapping(reads.map, target);
  let reading: PowerFeedReadingView | undefined;
  let readingUnavailableReason: string | undefined;
  if (mapping.feedId) {
    if (reads.feeds.status === "rejected") readingUnavailableReason = "read_failed";
    else ({ reading, reason: readingUnavailableReason } = feedReading(reads.feeds.value, mapping.feedId));
  }
  return {
    ...mapping,
    reading,
    readingUnavailableReason,
    recoveryCycle: reads.cycle.status === "fulfilled" ? recoveryCycle(reads.cycle.value) : undefined,
    observedAt,
  };
}
