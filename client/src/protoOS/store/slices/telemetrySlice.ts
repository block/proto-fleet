import { enableMapSet } from "immer";
import type { StateCreator } from "zustand";
import type {
  AsicTelemetryData,
  HashboardTelemetryData,
  Measurement,
  MetricTimeSeries,
  MetricUnit,
  MinerTelemetryData,
} from "../types";
import { getAsicId } from "../utils/getAsicId";
import { type HardwareSlice } from "./hardwareSlice";
import { type UISlice } from "./uiSlice";
import type { TimeSeriesResponse } from "@/protoOS/api/generatedApi";

// Enable Map/Set support for Immer
enableMapSet();

// Helper functions for API data transformation
const parseISODateToTimestamp = (isoString?: string): number | undefined => {
  if (!isoString) return undefined;
  return new Date(isoString).getTime();
};

const parseISO8601DurationToMs = (duration?: string): number => {
  if (!duration) return 60000; // Default to 1 minute

  // Parse ISO 8601 duration format like "PT15M", "PT1H", "PT5M"
  const match = duration.match(/PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?/);
  if (!match) return 60000; // Default to 1 minute

  const hours = parseInt(match[1] || "0", 10);
  const minutes = parseInt(match[2] || "0", 10);
  const seconds = parseInt(match[3] || "0", 10);

  return hours * 60 * 60 * 1000 + minutes * 60 * 1000 + seconds * 1000;
};

const createMeasurement = (
  value: number | undefined,
  units: MetricUnit,
): Measurement | undefined => {
  if (value === undefined) return undefined;
  return {
    value,
    units,
  };
};

// =============================================================================
// Telemetry Slice Interface
// =============================================================================

export interface TelemetrySlice {
  // State - normalized data structure
  miner: MinerTelemetryData | null;
  hashboards: Map<string, HashboardTelemetryData>;
  asics: Map<string, AsicTelemetryData>;
  lastApiResponse: any | null; // Store the API response
  lastUpdated: number;
  intervalMs: number; // sampling interval from API

  // Data Update Actions
  updateTelemetryData: (apiResponse: TimeSeriesResponse) => void;

  // need this because time series API currently doesn not return
  // inlet/outlet temps so we need to get them from separate API call
  updateHashboardTemperatures: (
    hashboardSerial: string,
    inletTemp?: Measurement,
    outletTemp?: Measurement,
    avgAsicTemp?: Measurement,
    maxAsicTemp?: Measurement,
  ) => void;

  // Utility Actions
  clearOldData: (olderThanTimestamp: number) => void;
  clearAllData: () => void;
}

// =============================================================================
// Telemetry Slice Implementation
// =============================================================================

export const createTelemetrySlice: StateCreator<
  { hardware: HardwareSlice; telemetry: TelemetrySlice; ui: UISlice },
  [["zustand/immer", never]],
  [],
  TelemetrySlice
> = (set) => ({
  // Initial state - normalized structure
  miner: null,
  hashboards: new Map(),
  asics: new Map(),
  lastApiResponse: null,
  lastUpdated: Date.now(),
  intervalMs: 900000, // Default 15 minutes

  // Primary data update method - transforms and stores API response
  updateTelemetryData: (apiResponse: TimeSeriesResponse) =>
    set((state) => {
      // adds ms equivalent to ISO8601 timestamps returned by API
      const transformedApiResponse = {
        ...apiResponse,
        meta: {
          ...apiResponse.meta,
          ...(apiResponse.meta?.start_time && {
            start_time_ms: new Date(apiResponse.meta.start_time).getTime(),
          }),
          ...(apiResponse.meta?.end_time && {
            end_time_ms: new Date(apiResponse.meta.end_time).getTime(),
          }),
        },
      };

      const now = Date.now();
      state.telemetry.lastApiResponse = transformedApiResponse;
      state.telemetry.lastUpdated = now;
      state.telemetry.intervalMs = parseISO8601DurationToMs(
        transformedApiResponse.meta?.interval,
      );

      const startTime = parseISODateToTimestamp(
        transformedApiResponse.meta?.start_time,
      );
      const endTime = parseISODateToTimestamp(
        transformedApiResponse.meta?.end_time,
      );

      // Validate that we have required timestamp data
      if (startTime === undefined || endTime === undefined) {
        console.warn(
          "Missing start_time or end_time in API response, skipping update",
        );
        return;
      }

      // Initialize miner if it doesn't exist
      if (!state.telemetry.miner) {
        state.telemetry.miner = {
          controlBoardSerial: "MAIN_001", // TODO: [STORE_REFACTOR] get from API
          hashboards: [],
        };
      }

      // Update miner metrics
      if (transformedApiResponse.data?.miner) {
        const minerData = transformedApiResponse.data.miner;

        Object.keys(minerData).forEach((field) => {
          // Only process known metric fields
          // TODO: [STORE_REFACTOR] could make this more generic so we dont have to add to this list every time we add a new metric
          const knownMetrics = [
            "hashrate",
            "temperature",
            "power",
            "efficiency",
          ];
          if (knownMetrics.includes(field)) {
            const metric = minerData[field];
            (state.telemetry.miner![
              field as keyof MinerTelemetryData
            ] as MetricTimeSeries) = {
              aggregates: metric.aggregates
                ? {
                    min: createMeasurement(
                      metric.aggregates.min,
                      metric.unit as MetricUnit,
                    ),
                    avg: createMeasurement(
                      metric.aggregates.avg,
                      metric.unit as MetricUnit,
                    ),
                    max: createMeasurement(
                      metric.aggregates.max,
                      metric.unit as MetricUnit,
                    ),
                  }
                : undefined,
              units: metric.unit as MetricUnit,
              values: metric.values || [],
              startTime,
              endTime,
            };
          }
        });
      }

      // Update hashboards
      if (transformedApiResponse.data?.hashboards) {
        const hashboardIds: string[] = [];
        transformedApiResponse.data.hashboards.forEach((hashboardData) => {
          const hashboardId = hashboardData.serial_number;
          if (!hashboardId) {
            console.warn(
              "Hashboard data missing serial_number:",
              hashboardData,
            );
            return;
          }

          hashboardIds.push(hashboardId);

          // TODO: [STORE_REFACTOR] generated API currently types serial_number as optional
          // even though it should always be present. We should fix this in the API spec
          // and then we can remove check for serial_number here.

          // Create or update hashboard
          if (
            !state.telemetry.hashboards.has(hashboardId) &&
            hashboardData.serial_number
          ) {
            state.telemetry.hashboards.set(hashboardId, {
              serial: hashboardData.serial_number,
            });
          }

          const hashboard = state.telemetry.hashboards.get(hashboardId)!;

          // Update hashboard metrics
          Object.keys(hashboardData).forEach((field) => {
            // Only process known metric fields, skip metadata fields
            const knownMetrics = [
              "hashrate",
              "temperature",
              "power",
              "efficiency",
            ];
            if (knownMetrics.includes(field)) {
              const metric = hashboardData[field];
              (hashboard[
                field as keyof HashboardTelemetryData
              ] as MetricTimeSeries) = {
                aggregates: metric.aggregates
                  ? {
                      min: createMeasurement(
                        metric.aggregates.min,
                        metric.unit,
                      ),
                      avg: createMeasurement(
                        metric.aggregates.avg,
                        metric.unit,
                      ),
                      max: createMeasurement(
                        metric.aggregates.max,
                        metric.unit,
                      ),
                    }
                  : undefined,
                units: metric.unit,
                values: metric.values || [],
                startTime,
                endTime,
              };
            }
          });
        });

        // Update miner's hashboard reference
        state.telemetry.miner!.hashboards = hashboardIds;
      }

      // Update ASICs
      if (transformedApiResponse.data?.asics) {
        transformedApiResponse.data?.asics.forEach((asicData) => {
          // Validate required fields
          if (
            asicData.index === undefined ||
            asicData.hashboard_index === undefined
          ) {
            console.warn(
              "ASIC data missing required index or hashboard_index:",
              asicData,
            );
            return;
          }

          // TODO: [STORE_REFACTOR] could implement a simple cache Map here so
          // that we dont have to iterate through all hashboards for every asic
          // alternatively when we could key hashboards by their index instead of serial
          // but since we currenlty still need to use some of the old apis that depend on serial number
          // we cant do that yet.
          const hashboardSerialNumber =
            transformedApiResponse.data?.hashboards?.find(
              (hb) => hb.index === asicData.hashboard_index,
            )?.serial_number;
          if (!hashboardSerialNumber) {
            console.warn(
              `Hashboard serial number not found for ASIC with index ${asicData.index}`,
            );
            return;
          }

          //TODO: [STORE_REFACTOR] MDK-API.json needs updated to always provide asic index
          const asicId = getAsicId(
            hashboardSerialNumber,
            asicData.index.toString(),
          );

          // Create or update ASIC
          if (!state.telemetry.asics.has(asicId)) {
            state.telemetry.asics.set(asicId, {
              id: asicId,
            });
          }

          const asic = state.telemetry.asics.get(asicId)!;

          // Update ASIC metrics
          Object.keys(asicData).forEach((field) => {
            // Only process known metric fields, skip metadata fields
            const knownMetrics = [
              "hashrate",
              "temperature",
              "power",
              "efficiency",
            ];
            if (knownMetrics.includes(field)) {
              const metric = asicData[field];
              (asic[field as keyof AsicTelemetryData] as MetricTimeSeries) = {
                aggregates: metric.aggregates
                  ? {
                      min: createMeasurement(
                        metric.aggregates.min,
                        metric.unit,
                      ),
                      avg: createMeasurement(
                        metric.aggregates.avg,
                        metric.unit,
                      ),
                      max: createMeasurement(
                        metric.aggregates.max,
                        metric.unit,
                      ),
                    }
                  : undefined,
                units: metric.unit,
                values: metric.values,
                startTime,
                endTime,
              };
            }
          });
        });
      }
    }),

  // Utility Actions
  clearOldData: (olderThanTimestamp) =>
    set((state) => {
      if (state.telemetry.lastUpdated < olderThanTimestamp) {
        state.telemetry.lastApiResponse = null;
      }

      // Clear old time series data
      if (state.telemetry.miner) {
        Object.keys(state.telemetry.miner).forEach((key) => {
          const metric =
            state.telemetry.miner![key as keyof MinerTelemetryData];
          // Only clear if this is actually a MetricTimeSeries (has endTime property)
          if (
            metric &&
            typeof metric === "object" &&
            "endTime" in metric &&
            typeof metric.endTime === "number"
          ) {
            if (metric.endTime < olderThanTimestamp) {
              // Type assertion is safe here because we've verified it's a MetricTimeSeries
              (state.telemetry.miner![key as keyof MinerTelemetryData] as
                | MetricTimeSeries
                | undefined) = undefined;
            }
          }
        });
      }

      // Clear old hashboard data
      for (const [, hashboard] of state.telemetry.hashboards) {
        Object.keys(hashboard).forEach((key) => {
          const metric = hashboard[key as keyof HashboardTelemetryData];
          // Only clear if this is actually a MetricTimeSeries (has endTime property)
          if (
            metric &&
            typeof metric === "object" &&
            "endTime" in metric &&
            typeof metric.endTime === "number"
          ) {
            if (metric.endTime < olderThanTimestamp) {
              (hashboard[key as keyof HashboardTelemetryData] as
                | MetricTimeSeries
                | undefined) = undefined;
            }
          }
        });
      }

      // Clear old asic data
      for (const [, asic] of state.telemetry.asics) {
        Object.keys(asic).forEach((key) => {
          const metric = asic[key as keyof AsicTelemetryData];
          // Only clear if this is actually a MetricTimeSeries (has endTime property)
          if (
            metric &&
            typeof metric === "object" &&
            "endTime" in metric &&
            typeof metric.endTime === "number"
          ) {
            if (metric.endTime < olderThanTimestamp) {
              (asic[key as keyof AsicTelemetryData] as
                | MetricTimeSeries
                | undefined) = undefined;
            }
          }
        });
      }
    }),

  // Update optional hashboard temperature sensors
  updateHashboardTemperatures: (
    hashboardSerial,
    inletTemp,
    outletTemp,
    avgAsicTemp,
    maxAsicTemp,
  ) =>
    set((state) => {
      let hashboard = state.telemetry.hashboards.get(hashboardSerial);

      // Create hashboard if it doesn't exist
      if (!hashboard) {
        hashboard = {
          serial: hashboardSerial,
        };
        state.telemetry.hashboards.set(hashboardSerial, hashboard);
      }

      // Update temperature data
      hashboard.inletTemp = inletTemp;
      hashboard.outletTemp = outletTemp;
      hashboard.avgAsicTemp = avgAsicTemp;
      hashboard.maxAsicTemp = maxAsicTemp;
    }),

  // Clear all telemetry data (useful when duration changes)
  clearAllData: () =>
    set((state) => {
      state.telemetry.miner = null;
      state.telemetry.hashboards = new Map();
      state.telemetry.asics = new Map();
      state.telemetry.lastApiResponse = null;
      state.telemetry.lastUpdated = Date.now();
    }),
});
