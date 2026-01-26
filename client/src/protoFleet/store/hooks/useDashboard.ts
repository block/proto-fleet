import { useShallow } from "zustand/react/shallow";

import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import type {
  Metric,
  MinerStateCounts,
  TemperatureStatusCount,
  UptimeStatusCount,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

// =============================================================================
// Data Selector Hooks
// =============================================================================

/**
 * Get metrics for a specific measurement type (Hashrate, Power, Efficiency)
 * Reads from combined metrics array (historical + streaming already merged)
 * Returns undefined if data hasn't loaded yet
 * Uses shallow comparison to prevent re-renders when filtered array contents haven't changed
 */
export const usePanelMetrics = (measurementType: MeasurementType): Metric[] | undefined => {
  return useFleetStore(
    useShallow((state) => {
      const metrics = state.dashboard.metrics;

      // If undefined, data hasn't loaded yet
      if (metrics === undefined) return undefined;

      // Filter the loaded metrics
      return metrics.filter((m) => m.measurementType === measurementType);
    }),
  );
};

/**
 * Get temperature status counts (already combined)
 * Only subscribes to temperature status count changes
 * Returns undefined if data hasn't loaded yet
 */
export const useTemperatureStatusCounts = (): TemperatureStatusCount[] | undefined => {
  return useFleetStore((state) => state.dashboard.temperatureStatusCounts);
};

/**
 * Get uptime status counts (already combined)
 * Only subscribes to uptime status count changes
 * Returns undefined if data hasn't loaded yet
 */
export const useUptimeStatusCounts = (): UptimeStatusCount[] | undefined => {
  return useFleetStore((state) => state.dashboard.uptimeStatusCounts);
};

/**
 * Get miner state counts from streaming
 * Returns undefined if data hasn't loaded yet
 */
export const useMinerStateCounts = (): MinerStateCounts | undefined => {
  return useFleetStore((state) => state.dashboard.minerStateCounts);
};

/**
 * Get dashboard error state
 */
export const useDashboardError = (): Error | null => {
  return useFleetStore((state) => state.dashboard.error);
};

// =============================================================================
// Action Hooks
// =============================================================================

/**
 * Set historical metrics (replaces existing)
 */
export const useSetHistoricalMetrics = () => {
  return useFleetStore((state) => state.dashboard.setHistoricalMetrics);
};

/**
 * Append streaming metrics (merges with existing)
 */
export const useAppendStreamingMetrics = () => {
  return useFleetStore((state) => state.dashboard.appendStreamingMetrics);
};

/**
 * Set historical temperature counts (replaces existing)
 */
export const useSetHistoricalTemperatureCounts = () => {
  return useFleetStore((state) => state.dashboard.setHistoricalTemperatureCounts);
};

/**
 * Append streaming temperature counts (merges with existing)
 */
export const useAppendStreamingTemperatureCounts = () => {
  return useFleetStore((state) => state.dashboard.appendStreamingTemperatureCounts);
};

/**
 * Set historical uptime counts (replaces existing)
 */
export const useSetHistoricalUptimeCounts = () => {
  return useFleetStore((state) => state.dashboard.setHistoricalUptimeCounts);
};

/**
 * Append streaming uptime counts (merges with existing)
 */
export const useAppendStreamingUptimeCounts = () => {
  return useFleetStore((state) => state.dashboard.appendStreamingUptimeCounts);
};

/**
 * Set all historical data atomically (prevents race conditions)
 * Replaces metrics, temperature counts, and uptime counts in a single state update
 */
export const useSetAllHistoricalData = () => {
  return useFleetStore((state) => state.dashboard.setAllHistoricalData);
};

/**
 * Set miner state counts from streaming
 */
export const useSetMinerStateCounts = () => {
  return useFleetStore((state) => state.dashboard.setMinerStateCounts);
};

/**
 * Clear duration-dependent metrics (used when duration changes)
 * Sets metrics back to undefined to indicate loading state
 * Note: Does NOT clear minerStateCounts as those are independent of time range
 */
export const useClearMetrics = () => {
  return useFleetStore((state) => state.dashboard.clearMetrics);
};

/**
 * Set dashboard error state
 */
export const useSetDashboardError = () => {
  return useFleetStore((state) => state.dashboard.setError);
};
