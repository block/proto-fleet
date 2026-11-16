import { useMemo } from "react";
import { useGroupedErrors, useMiningStatus } from "@/protoOS/store";
import {
  type GroupedStatusErrors,
  useMinerStatusSummary as useSharedMinerStatusSummary,
} from "@/shared/hooks/useStatusSummary";

/**
 * Generates a holistic status summary based on errors and mining status
 * @returns Status summary text like "Hashing", "Sleeping", "Fan issue", etc.
 */
export const useMinerStatusSummary = (): string => {
  const miningStatus = useMiningStatus();
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
      // Plant alarms are container-wide, so they count as their own issue rather than a component failure
      other: groupedErrors.plant.map(() => ({ componentType: "other" })),
    }),
    [groupedErrors],
  );

  // Determine isSleeping from mining status
  // ProtoOS is always online (you can only see it if connected), so isOffline is always false
  const isSleeping = miningStatus === "poweringOff" || miningStatus === "stopped";
  const isCurtailed = miningStatus === "curtailed";

  const summary = useSharedMinerStatusSummary(sharedErrors, isSleeping);
  // Curtailed is a ProtoOS-specific state the shared summary doesn't model:
  // the rig is on but mining is paused by the curtailment service. Surface it
  // at the same priority as sleeping so the header doesn't claim "Hashing".
  if (isCurtailed) {
    return "Curtailed";
  }
  return summary.condensed;
};
