import { timestampDate } from "@bufbuild/protobuf/wkt";
import {
  type GetModulePowerFeedResponse,
  PowerFeedMappingConfidence,
  PowerFeedState,
} from "@/protoFleet/api/generated/containers/v1/containers_pb";
import type { PowerFeedConfidence, PowerFeedView } from "@/shared/components/PowerFeedCard";

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
