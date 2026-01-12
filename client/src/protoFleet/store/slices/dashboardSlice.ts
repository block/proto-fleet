import type { StateCreator } from "zustand";
import type { FleetStore } from "../useFleetStore";
import { ComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import type {
  Metric,
  TemperatureStatusCount,
  UptimeStatusCount,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { mergeByTimestamp, mergeStatusCounts } from "@/protoFleet/features/dashboard/utils/telemetryMerge";

// =============================================================================
// Types
// =============================================================================

export interface ComponentErrorState {
  // Track counts for display (number of devices with errors per component type)
  counts: Partial<Record<ComponentType, number>>;

  // Track which devices have errors for each component type
  devicesByComponent: Partial<Record<ComponentType, Set<string>>>;

  // Track error IDs per device per component for proper CLOSED event handling
  errorIdsByDeviceAndComponent: Partial<Record<ComponentType, Record<string, Set<string>>>>;
}

// =============================================================================
// Dashboard Slice Interface
// =============================================================================

export interface DashboardSlice {
  // Combined metrics (historical + streaming merged at write-time)
  // undefined = not loaded yet, array = loaded (empty or populated)
  metrics: Metric[] | undefined;
  temperatureStatusCounts: TemperatureStatusCount[] | undefined;
  uptimeStatusCounts: UptimeStatusCount[] | undefined;

  // Component error tracking for dashboard display
  componentErrors: ComponentErrorState;

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

  // Component error actions
  setComponentErrorCounts: (
    counts: Partial<Record<ComponentType, number>>,
    deviceErrorMap?: Partial<Record<ComponentType, Record<string, string[]>>>,
  ) => void;
  handleComponentErrorStream: (
    event: "OPENED" | "UPDATED" | "CLOSED",
    deviceId: string,
    componentType: ComponentType,
    errorId: string,
  ) => void;
  clearComponentErrors: () => void;
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

  // Component error initial state
  componentErrors: {
    counts: {},
    devicesByComponent: {},
    errorIdsByDeviceAndComponent: {},
  },

  // Actions - Metrics
  setHistoricalMetrics: (metrics) =>
    set((state) => {
      state.dashboard.metrics = metrics;
    }),

  appendStreamingMetrics: (newMetrics) =>
    set((state) => {
      // Don't initialize from streaming data - wait for historical data to load first
      // This prevents "No Data" flash when streaming arrives before historical
      if (state.dashboard.metrics === undefined) {
        return;
      }
      state.dashboard.metrics = mergeByTimestamp(state.dashboard.metrics, newMetrics);
    }),

  // Actions - Temperature Status Counts
  setHistoricalTemperatureCounts: (counts) =>
    set((state) => {
      state.dashboard.temperatureStatusCounts = counts;
    }),

  appendStreamingTemperatureCounts: (newCounts) =>
    set((state) => {
      // Don't initialize from streaming data - wait for historical data to load first
      if (state.dashboard.temperatureStatusCounts === undefined) {
        return;
      }
      state.dashboard.temperatureStatusCounts = mergeStatusCounts(state.dashboard.temperatureStatusCounts, newCounts);
    }),

  // Actions - Uptime Status Counts
  setHistoricalUptimeCounts: (counts) =>
    set((state) => {
      state.dashboard.uptimeStatusCounts = counts;
    }),

  appendStreamingUptimeCounts: (newCounts) =>
    set((state) => {
      // Don't initialize from streaming data - wait for historical data to load first
      if (state.dashboard.uptimeStatusCounts === undefined) {
        return;
      }
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

  // Component error actions
  setComponentErrorCounts: (counts, deviceErrorMap) =>
    set((state) => {
      // Initialize from API response
      state.dashboard.componentErrors.counts = counts;
      state.dashboard.componentErrors.devicesByComponent = {};
      state.dashboard.componentErrors.errorIdsByDeviceAndComponent = {};

      // Initialize tracking structures
      Object.keys(counts).forEach((key) => {
        const componentType = Number(key) as ComponentType;

        // Track devices for this component
        const devices = Object.keys(deviceErrorMap?.[componentType] || {});
        state.dashboard.componentErrors.devicesByComponent[componentType] = new Set(devices);

        // Track error IDs per device
        if (deviceErrorMap?.[componentType]) {
          state.dashboard.componentErrors.errorIdsByDeviceAndComponent[componentType] = {};
          Object.entries(deviceErrorMap[componentType]).forEach(([deviceId, errorIds]) => {
            state.dashboard.componentErrors.errorIdsByDeviceAndComponent[componentType]![deviceId] = new Set(errorIds);
          });
        }
      });
    }),

  handleComponentErrorStream: (event, deviceId, componentType, errorId) =>
    set((state) => {
      const { counts, devicesByComponent, errorIdsByDeviceAndComponent } = state.dashboard.componentErrors;

      // Ensure we have tracking structures for this component type
      if (!devicesByComponent[componentType]) {
        devicesByComponent[componentType] = new Set();
      }
      if (!errorIdsByDeviceAndComponent[componentType]) {
        errorIdsByDeviceAndComponent[componentType] = {};
      }

      const deviceSet = devicesByComponent[componentType]!;
      const errorsByDevice = errorIdsByDeviceAndComponent[componentType]!;

      // Ensure we have a Set for this device's errors
      if (!errorsByDevice[deviceId]) {
        errorsByDevice[deviceId] = new Set();
      }

      const deviceErrorSet = errorsByDevice[deviceId];

      if (event === "CLOSED") {
        // Remove error from device
        if (deviceErrorSet.has(errorId)) {
          deviceErrorSet.delete(errorId);

          // If device has no more errors for this component, remove from device set
          if (deviceErrorSet.size === 0) {
            deviceSet.delete(deviceId);
            delete errorsByDevice[deviceId];

            // Update count (number of devices with errors)
            counts[componentType] = deviceSet.size;

            // Clean up if no devices have this component error
            if (deviceSet.size === 0) {
              delete counts[componentType];
              delete devicesByComponent[componentType];
              delete errorIdsByDeviceAndComponent[componentType];
            }
          }
        }
      } else {
        // OPENED or UPDATED
        const isNewDevice = !deviceSet.has(deviceId);

        // Add error to device's error set
        deviceErrorSet.add(errorId);

        // If this is the first error for this device-component, increment count
        if (isNewDevice) {
          deviceSet.add(deviceId);
          counts[componentType] = deviceSet.size;
        }
      }
    }),

  clearComponentErrors: () =>
    set((state) => {
      state.dashboard.componentErrors = {
        counts: {},
        devicesByComponent: {},
        errorIdsByDeviceAndComponent: {},
      };
    }),
});
