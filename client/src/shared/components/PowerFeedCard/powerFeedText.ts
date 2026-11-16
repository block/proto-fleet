export type PowerFeedConfidence = "wiring_verified" | "configured" | "ambiguous" | "unavailable";
export type PowerFeedState = "enabled" | "disabled" | "unknown";
export type PowerFeedModuleRef = { tank: number; module: number };

export const confidenceLabels: Record<PowerFeedConfidence, string> = {
  wiring_verified: "Wiring verified",
  configured: "Configured",
  ambiguous: "Ambiguous",
  unavailable: "Unavailable",
};

export const stateLabels: Record<PowerFeedState, string> = {
  enabled: "Enabled",
  disabled: "Disabled",
  unknown: "Unknown",
};

const mappingReasons: Record<string, string> = {
  topology_unavailable: "The controller map is unavailable.",
  module_not_in_topology: "The controller does not report this module position.",
  duplicate_module: "The controller reports this module position more than once.",
  module_identity_unconfirmed: "The module's identity has not been confirmed.",
  module_identity_changed: "A different module is now reported at this position.",
  slot_placement_invalid: "A board reports an invalid slot position.",
  configured_location_mismatch: "A slot is configured for a different module.",
  no_feed_assigned: "No feed is assigned to this module.",
  multiple_feeds: "Boards in this module report different feeds.",
  feed_evidence_incomplete: "A board in this module reports no feed.",
};

const readingReasons: Record<string, string> = {
  snapshot_not_ready: "The controller has not collected feed readings yet.",
  not_in_snapshot: "The controller has no reading for this feed.",
  duplicate_records: "The controller reports this feed more than once.",
  read_failed: "Feed readings could not be read.",
};

export const moduleRefLabel = ({ tank, module }: PowerFeedModuleRef) => `T${tank} M${module}`;

/** Operator text for a mapping reason; unknown reasons are shown verbatim. */
export function mappingReasonText(reason: string, sharedWith: PowerFeedModuleRef[] = []): string {
  if (reason === "feed_shared")
    return sharedWith.length
      ? `This feed is also assigned to ${sharedWith.map(moduleRefLabel).join(", ")}.`
      : "This feed is also assigned to another module.";
  if (reason.startsWith("wiring_")) return `Switch-port wiring check reports ${reason.slice(7).replaceAll("_", " ")}.`;
  return mappingReasons[reason] ?? `Mapping problem: ${reason}.`;
}

export function readingReasonText(reason: string): string {
  return readingReasons[reason] ?? "No current reading.";
}
