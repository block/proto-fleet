import type { StateCreator } from "zustand";
import type { FleetStore } from "../useFleetStore";
import type {
  Metric,
  TemperatureStatusCount,
  UptimeStatusCount,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { mergeByTimestamp, mergeStatusCounts } from "@/protoFleet/features/dashboard/utils/telemetryMerge";

// =============================================================================
// Dashboard Slice Interface
// =============================================================================

export interface DashboardSlice {
  // Combined metrics (historical + streaming merged at write-time)
  // undefined = not loaded yet, array = loaded (empty or populated)
  metrics: Metric[] | undefined;
  temperatureStatusCounts: TemperatureStatusCount[] | undefined;
  uptimeStatusCounts: UptimeStatusCount[] | undefined;

  // Error state
  error: Error | null;

  // Actions
  setHistoricalMetrics: (metrics: Metric[]) => void;
  appendStreamingMetrics: (metrics: Metric[]) => void;
  setHistoricalTemperatureCounts: (counts: TemperatureStatusCount[]) => void;
  appendStreamingTemperatureCounts: (counts: TemperatureStatusCount[]) => void;
  setHistoricalUptimeCounts: (counts: UptimeStatusCount[]) => void;
  appendStreamingUptimeCounts: (counts: UptimeStatusCount[]) => void;
  setAllHistoricalData: (
    metrics: Metric[],
    temperatureCounts: TemperatureStatusCount[],
    uptimeCounts: UptimeStatusCount[],
  ) => void;
  clearMetrics: () => void;
  setError: (error: Error | null) => void;
}

// =============================================================================
// Dashboard Slice Implementation
// =============================================================================

export const createDashboardSlice: StateCreator<FleetStore, [["zustand/immer", never]], [], DashboardSlice> = (
  set,
) => ({
  // Initial state - undefined indicates data hasn't loaded yet
  metrics: undefined,
  temperatureStatusCounts: undefined,
  uptimeStatusCounts: undefined,
  error: null,

  // Actions - Metrics
  setHistoricalMetrics: (metrics) =>
    set((state) => {
      state.dashboard.metrics = metrics;
    }),

  appendStreamingMetrics: (newMetrics) =>
    set((state) => {
      // If metrics are undefined, initialize with new metrics
      if (state.dashboard.metrics === undefined) {
        state.dashboard.metrics = newMetrics;
      } else {
        state.dashboard.metrics = mergeByTimestamp(state.dashboard.metrics, newMetrics);
      }
    }),

  // Actions - Temperature Status Counts
  setHistoricalTemperatureCounts: (counts) =>
    set((state) => {
      state.dashboard.temperatureStatusCounts = counts;
    }),

  appendStreamingTemperatureCounts: (newCounts) =>
    set((state) => {
      state.dashboard.temperatureStatusCounts = mergeStatusCounts(state.dashboard.temperatureStatusCounts, newCounts);
    }),

  // Actions - Uptime Status Counts
  setHistoricalUptimeCounts: (counts) =>
    set((state) => {
      state.dashboard.uptimeStatusCounts = counts;
    }),

  appendStreamingUptimeCounts: (newCounts) =>
    set((state) => {
      state.dashboard.uptimeStatusCounts = mergeStatusCounts(state.dashboard.uptimeStatusCounts, newCounts);
    }),

  // Atomic action - Set all historical data at once to prevent race conditions
  setAllHistoricalData: (metrics, temperatureCounts, uptimeCounts) =>
    set((state) => {
      state.dashboard.metrics = metrics;
      state.dashboard.temperatureStatusCounts = temperatureCounts;
      state.dashboard.uptimeStatusCounts = uptimeCounts;
    }),

  // Actions - Utility
  clearMetrics: () =>
    set((state) => {
      state.dashboard.metrics = undefined;
      state.dashboard.temperatureStatusCounts = undefined;
      state.dashboard.uptimeStatusCounts = undefined;
    }),

  setError: (error) =>
    set((state) => {
      state.dashboard.error = error;
    }),
});
