import { useMemo } from "react";
import { useGroupedErrors, useMinerStore } from "@/protoOS/store";
import {
  type GroupedStatusErrors,
  useMinerStatusSummary as useSharedMinerStatusSummary,
} from "@/shared/hooks/useStatusSummary";

/**
 * Generates a holistic status summary based on errors and mining status
 * @returns Status summary text like "Hashing", "Sleeping", "Fan issue", etc.
 */
export const useMinerStatusSummary = (): string => {
  const miningStatus = useMinerStore((state) => state.minerStatus.miningStatus);
  const groupedErrors = useGroupedErrors();

  // Transform ProtoOS errors to shared format
  const sharedErrors = useMemo<GroupedStatusErrors>(
    () => ({
      hashboard: groupedErrors.hashboard.map((e) => ({
        componentType: "hashboard",
        componentIndex: e.componentIndex,
      })),
      psu: groupedErrors.psu.map((e) => ({
        componentType: "psu",
        componentIndex: e.componentIndex,
      })),
      fan: groupedErrors.fan.map((e) => ({
        componentType: "fan",
        componentIndex: e.componentIndex,
      })),
      // Map 'system' errors to 'controlBoard' for shared format
      controlBoard: groupedErrors.system.map((e) => ({
        componentType: "controlBoard",
        componentIndex: e.componentIndex,
      })),
    }),
    [groupedErrors],
  );

  // Determine isSleeping from mining status
  // ProtoOS is always online (you can only see it if connected), so isOffline is always false
  const isSleeping = /PoweringOff|Stopped/i.test(miningStatus || "");

  const summary = useSharedMinerStatusSummary(sharedErrors, isSleeping);
  return summary.condensed;
};
