import { useMemo } from "react";
import { useGroupedErrors } from "@/protoOS/store";
import {
  type GroupedStatusErrors,
  type MinerStatusSummary,
  useMinerStatusSummary as useSharedMinerStatusSummary,
} from "@/shared/hooks/useStatusSummary";

/**
 * Returns title describing the current miner status
 * @returns Object with title and optional subtitle strings
 */
export const useMinerStatusTitle = (): { title: string; subtitle?: string } => {
  const groupedErrors = useGroupedErrors();

  // Transform ProtoOS errors to shared format
  const sharedErrors = useMemo<GroupedStatusErrors>(
    () => ({
      hashboard: groupedErrors.hashboard.map((e) => ({
        componentType: "hashboard",
        slot: e.slot,
      })),
      psu: groupedErrors.psu.map((e) => ({
        componentType: "psu",
        slot: e.slot,
      })),
      fan: groupedErrors.fan.map((e) => ({
        componentType: "fan",
        slot: e.slot,
      })),
      // Map 'system' errors to 'controlBoard' for shared format
      controlBoard: groupedErrors.system.map((e) => ({
        componentType: "controlBoard",
        slot: e.slot,
      })),
      other: [],
    }),
    [groupedErrors],
  );

  // Use shared hook - title doesn't depend on sleeping/offline status
  const summary: MinerStatusSummary = useSharedMinerStatusSummary(sharedErrors);

  // Return with empty subtitle to maintain backward compatibility
  // Note: MinerStatusModalContent will handle showing "Miner is asleep" title when isSleeping is true
  return useMemo(
    () => ({
      title: summary.title,
      subtitle: summary.subtitle,
    }),
    [summary.title, summary.subtitle],
  );
};
