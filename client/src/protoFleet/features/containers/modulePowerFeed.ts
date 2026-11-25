import { timestampDate } from "@bufbuild/protobuf/wkt";
import {
  type GetModulePowerFeedResponse,
  PowerFeedAction,
  PowerFeedMappingConfidence,
  PowerFeedState,
} from "@/protoFleet/api/generated/containers/v1/containers_pb";
import type {
  PowerFeedAction as PowerFeedActionName,
  PowerFeedConfidence,
  PowerFeedView,
} from "@/shared/components/PowerFeedCard";

const confidences: Record<PowerFeedMappingConfidence, PowerFeedConfidence> = {
  [PowerFeedMappingConfidence.UNSPECIFIED]: "unavailable",
  [PowerFeedMappingConfidence.WIRING_VERIFIED]: "wiring_verified",
  [PowerFeedMappingConfidence.CONFIGURED]: "configured",
  [PowerFeedMappingConfidence.AMBIGUOUS]: "ambiguous",
  [PowerFeedMappingConfidence.UNAVAILABLE]: "unavailable",
};

/** Maps the server's live feed read onto the shared card's neutral view. */
export function modulePowerFeedView(response: GetModulePowerFeedResponse): PowerFeedView {
  const mapping = response.mapping;
  const feed = response.feed;
  const cycle = response.recoveryCycle;
  return {
    feedId: mapping?.feedId || undefined,
    confidence: confidences[mapping?.confidence ?? PowerFeedMappingConfidence.UNSPECIFIED] ?? "unavailable",
    reasons: mapping?.reasons ?? [],
    sharedWith: (mapping?.sharedWith ?? []).map(({ tank, module }) => ({ tank, module })),
    reading: feed
      ? {
          state:
            feed.state === PowerFeedState.ENABLED
              ? "enabled"
              : feed.state === PowerFeedState.DISABLED
                ? "disabled"
                : "unknown",
          active: feed.active,
          faulted: feed.faulted,
          overridden: feed.overridden,
          voltageV: feed.voltageV,
          currentA: feed.currentA,
          powerW: feed.powerW,
          lineVoltagesV: [feed.voltageAbV, feed.voltageBcV, feed.voltageCaV],
          phaseCurrentsA: [feed.currentAA, feed.currentBA, feed.currentCA],
        }
      : undefined,
    readingUnavailableReason: response.feedUnavailableReason || undefined,
    recoveryCycle: !response.recoveryCycleKnown
      ? undefined
      : cycle
        ? {
            phase: cycle.phase,
            outletIds: cycle.outletIds,
            restoreAfter: cycle.restoreAfter ? timestampDate(cycle.restoreAfter) : undefined,
            lastError: cycle.lastError,
          }
        : null,
  };
}

export const powerFeedActions: Record<PowerFeedActionName, PowerFeedAction> = {
  turn_on: PowerFeedAction.TURN_ON,
  turn_off: PowerFeedAction.TURN_OFF,
  cycle: PowerFeedAction.CYCLE,
  clear_override: PowerFeedAction.CLEAR_OVERRIDE,
};

export type ModulePowerFeedControl = {
  feedId: string;
  actions: PowerFeedActionName[];
  /** Set when the actions cannot run now. */
  unavailableReason?: string;
  cycleHoldSeconds: number;
};

/** The controls the server offers for this read; undefined without permission or a mapped feed. */
export function modulePowerFeedControl(response: GetModulePowerFeedResponse): ModulePowerFeedControl | undefined {
  const control = response.control;
  if (!control) return undefined;
  const names = Object.entries(powerFeedActions) as [PowerFeedActionName, PowerFeedAction][];
  return {
    feedId: response.mapping?.feedId ?? "",
    actions: control.actions.flatMap((wire) => names.filter(([, value]) => value === wire).map(([name]) => name)),
    unavailableReason: control.unavailableReason || (response.mapping?.feedId ? undefined : "No feed is mapped."),
    cycleHoldSeconds: control.cycleHoldSeconds,
  };
}
