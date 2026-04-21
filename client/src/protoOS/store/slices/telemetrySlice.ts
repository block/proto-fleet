import { enableMapSet } from "immer";
import type { StateCreator } from "zustand";
import type {
  AsicTelemetryData,
  FanTelemetryData,
  HashboardTelemetryData,
  Measurement,
  MetricTelemetry,
  MetricTimeSeries,
  MetricUnit,
  MinerTelemetryData,
  PsuTelemetryData,
} from "../types";
import type { MinerStore } from "../useMinerStore";
import { getAsicId } from "../utils/getAsicId";
import type { CoolingStatusCoolingstatus, TelemetryData, TimeSeriesResponse } from "@/protoOS/api/generatedApi";

// Enable Map/Set support for Immer
enableMapSet();

// Type helpers to extract metric fields
type MetricKeys<T> = {
  [K in keyof T]: T[K] extends MetricTelemetry | undefined ? K : never;
}[keyof T] &
  string;

type MinerMetricKeys = MetricKeys<MinerTelemetryData>;
type HashboardMetricKeys = MetricKeys<HashboardTelemetryData>;
type AsicMetricKeys = MetricKeys<AsicTelemetryData>;

// Helper to check if a field is a metric field
const isMetricField = (obj: any, key: string): boolean => {
  const value = obj[key];
  return value && typeof value === "object" && "unit" in value;
};

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

const createMeasurement = (value: number | undefined, units: MetricUnit): Measurement | undefined => {
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
  psus: Map<number, PsuTelemetryData>;
  fans: Map<number, FanTelemetryData>;
  coolingMode: CoolingStatusCoolingstatus["fan_mode"] | null; // Current cooling mode (e.g., "Auto", "Off", "Manual")
  lastApiResponse: any | null; // Store the API response
  lastUpdated: number;
  intervalMs: number; // sampling interval from API

  // Data Update Actions
  updateTimeSeriesTelemetry: (apiResponse: TimeSeriesResponse) => void;
  updateLatestTelemetry: (telemetryData: TelemetryData) => void;

  // need this because time series API currently doesn not return
  // inlet/outlet temps so we need to get them from separate API call
  updateHashboardTemperatures: (
    hashboardSerial: string,
    inletTemp?: Measurement,
    outletTemp?: Measurement,
    avgAsicTemp?: Measurement,
    maxAsicTemp?: Measurement,
  ) => void;

  // PSU/Fan Update Actions
  updatePsuTelemetry: (psuId: number, telemetryData: Partial<PsuTelemetryData>) => void;
  updateFanTelemetry: (fanSlot: number, telemetryData: Partial<FanTelemetryData>) => void;

  // Cooling Mode Actions
  updateCoolingMode: (mode: CoolingStatusCoolingstatus["fan_mode"]) => void;

  // Utility Actions
  clearOldData: (olderThanTimestamp: number) => void;
  clearTimeSeriesData: () => void;
  clearLatestData: () => void;
  clearAllData: () => void;
}

// =============================================================================
// Telemetry Slice Implementation
// =============================================================================

export const createTelemetrySlice: StateCreator<MinerStore, [["zustand/immer", never]], [], TelemetrySlice> = (
  set,
) => ({
  // Initial state - normalized structure
  miner: null,
  hashboards: new Map(),
  asics: new Map(),
  psus: new Map(),
  fans: new Map(),
  coolingMode: null,
  lastApiResponse: null,
  lastUpdated: Date.now(),
  intervalMs: 900000, // Default 15 minutes

  // Update time series telemetry data from time series API
  updateTimeSeriesTelemetry: (apiResponse: TimeSeriesResponse) =>
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
      state.telemetry.intervalMs = parseISO8601DurationToMs(transformedApiResponse.meta?.interval);

      const startTime = parseISODateToTimestamp(transformedApiResponse.meta?.start_time);
      const endTime = parseISODateToTimestamp(transformedApiResponse.meta?.end_time);

      // Validate that we have required timestamp data
      if (startTime === undefined || endTime === undefined) {
        console.warn("Missing start_time or end_time in API response, skipping update");
        return;
      }

      // Initialize miner if it doesn't exist
      if (!state.telemetry.miner) {
        state.telemetry.miner = {
          hashboards: [],
        };
      }

      // Update miner metrics
      if (transformedApiResponse.data?.miner) {
        const minerData = transformedApiResponse.data.miner;

        Object.keys(minerData)
          .filter((key) => isMetricField(minerData, key))
          .forEach((field) => {
            const metric = minerData[field as MinerMetricKeys];

            // Get or create the metric object
            if (!state.telemetry.miner![field as MinerMetricKeys]) {
              state.telemetry.miner![field as MinerMetricKeys] = {};
            }

            const metricTelemetry = state.telemetry.miner![field as MinerMetricKeys] as MetricTelemetry;

            // Update only timeSeries, Immer preserves latest automatically
            metricTelemetry.timeSeries = {
              aggregates: metric.aggregates
                ? {
                    min: createMeasurement(metric.aggregates.min, metric.unit as MetricUnit),
                    avg: createMeasurement(metric.aggregates.avg, metric.unit as MetricUnit),
                    max: createMeasurement(metric.aggregates.max, metric.unit as MetricUnit),
                  }
                : undefined,
              units: metric.unit as MetricUnit,
              values: metric.values || [],
              startTime,
              endTime,
            };
          });
      }

      // Update hashboards
      if (transformedApiResponse.data?.hashboards) {
        const hashboardIds: string[] = [];
        transformedApiResponse.data.hashboards.forEach((hashboardData) => {
          const hashboardId = hashboardData.serial_number;
          if (!hashboardId) {
            console.warn("Hashboard data missing serial_number:", hashboardData);
            return;
          }

          hashboardIds.push(hashboardId);

          // TODO: [STORE_REFACTOR] generated API currently types serial_number as optional
          // even though it should always be present. We should fix this in the API spec
          // and then we can remove check for serial_number here.

          // Create or update hashboard
          if (!state.telemetry.hashboards.has(hashboardId) && hashboardData.serial_number) {
            state.telemetry.hashboards.set(hashboardId, {
              serial: hashboardData.serial_number,
            });
          }

          const hashboard = state.telemetry.hashboards.get(hashboardId)!;

          // Update hashboard metrics
          Object.keys(hashboardData)
            .filter((key) => isMetricField(hashboardData, key))
            .forEach((field) => {
              const metric = hashboardData[field as HashboardMetricKeys];

              // Get or create the metric object
              if (!hashboard[field as HashboardMetricKeys]) {
                hashboard[field as HashboardMetricKeys] = {};
              }

              const metricTelemetry = hashboard[field as HashboardMetricKeys] as MetricTelemetry;

              // Update only timeSeries, Immer preserves latest automatically
              metricTelemetry.timeSeries = {
                aggregates: metric.aggregates
                  ? {
                      min: createMeasurement(metric.aggregates.min, metric.unit),
                      avg: createMeasurement(metric.aggregates.avg, metric.unit),
                      max: createMeasurement(metric.aggregates.max, metric.unit),
                    }
                  : undefined,
                units: metric.unit,
                values: metric.values || [],
                startTime,
                endTime,
              };
            });
        });

        // Update miner's hashboard reference
        state.telemetry.miner!.hashboards = hashboardIds;
      }

      // Update ASICs
      if (transformedApiResponse.data?.asics) {
        transformedApiResponse.data?.asics.forEach((asicData) => {
          // Validate required fields
          if (asicData.index === undefined || asicData.hashboard_index === undefined) {
            console.warn("ASIC data missing required index or hashboard_index:", asicData);
            return;
          }

          // TODO: [STORE_REFACTOR] could implement a simple cache Map here so
          // that we dont have to iterate through all hashboards for every asic
          // alternatively when we could key hashboards by their index instead of serial
          // but since we currenlty still need to use some of the old apis that depend on serial number
          // we cant do that yet.
          const hashboardSerialNumber = transformedApiResponse.data?.hashboards?.find(
            (hb) => hb.index === asicData.hashboard_index,
          )?.serial_number;
          if (!hashboardSerialNumber) {
            console.warn(`Hashboard serial number not found for ASIC with index ${asicData.index}`);
            return;
          }

          //TODO: [STORE_REFACTOR] MDK-API.json needs updated to always provide asic index
          const asicId = getAsicId(hashboardSerialNumber, asicData.index.toString());

          // Create or update ASIC
          if (!state.telemetry.asics.has(asicId)) {
            state.telemetry.asics.set(asicId, {
              id: asicId,
            });
          }

          const asic = state.telemetry.asics.get(asicId)!;

          // Update ASIC metrics
          Object.keys(asicData)
            .filter((key) => isMetricField(asicData, key))
            .forEach((field) => {
              const metric = asicData[field as AsicMetricKeys];

              // Get or create the metric object
              if (!asic[field as AsicMetricKeys]) {
                asic[field as AsicMetricKeys] = {};
              }

              const metricTelemetry = asic[field as AsicMetricKeys] as MetricTelemetry;

              // Update only timeSeries, Immer preserves latest automatically
              metricTelemetry.timeSeries = {
                aggregates: metric.aggregates
                  ? {
                      min: createMeasurement(metric.aggregates.min, metric.unit),
                      avg: createMeasurement(metric.aggregates.avg, metric.unit),
                      max: createMeasurement(metric.aggregates.max, metric.unit),
                    }
                  : undefined,
                units: metric.unit,
                values: metric.values,
                startTime,
                endTime,
              };
            });
        });
      }
    }),

  // Update latest telemetry values from real-time telemetry API
  updateLatestTelemetry: (telemetryData: TelemetryData) =>
    set((state) => {
      const now = Date.now();
      state.telemetry.lastUpdated = now;

      // Initialize miner if it doesn't exist
      if (!state.telemetry.miner) {
        state.telemetry.miner = {
          hashboards: [],
        };
      }

      // Update miner latest metrics
      if (telemetryData.miner) {
        const minerData = telemetryData.miner;

        Object.keys(minerData)
          .filter((key) => isMetricField(minerData, key))
          .forEach((field) => {
            const metric = minerData[field as MinerMetricKeys];

            // Get or create the metric object
            if (!state.telemetry.miner![field as MinerMetricKeys]) {
              state.telemetry.miner![field as MinerMetricKeys] = {};
            }

            const metricTelemetry = state.telemetry.miner![field as MinerMetricKeys] as MetricTelemetry;

            // Update only latest, Immer preserves timeSeries automatically
            metricTelemetry.latest = createMeasurement(metric.value, metric.unit as MetricUnit);
          });
      }

      // Update hashboard latest metrics
      if (telemetryData.hashboards) {
        telemetryData.hashboards.forEach((hashboardData) => {
          const hashboardId = hashboardData.serial_number;
          if (!hashboardId) return;

          // Create or get hashboard
          if (!state.telemetry.hashboards.has(hashboardId)) {
            state.telemetry.hashboards.set(hashboardId, {
              serial: hashboardId,
            });
          }

          const hashboard = state.telemetry.hashboards.get(hashboardId)!;

          // Update standard metrics (excluding special temperature handling below)
          Object.keys(hashboardData)
            .filter((key) => key !== "temperature" && isMetricField(hashboardData, key))
            .forEach((field) => {
              const metric = hashboardData[field as keyof typeof hashboardData];
              if (metric && typeof metric === "object" && "value" in metric) {
                // Get or create the metric object
                if (!hashboard[field as HashboardMetricKeys]) {
                  hashboard[field as HashboardMetricKeys] = {};
                }

                const metricTelemetry = hashboard[field as HashboardMetricKeys] as MetricTelemetry;

                // Update only latest, Immer preserves timeSeries automatically
                metricTelemetry.latest = createMeasurement(metric.value, metric.unit as MetricUnit);
              }
            });

          // Update temperature metrics (special handling for hashboard temperature structure)
          if (hashboardData.temperature) {
            const temp = hashboardData.temperature;

            // Update aggregate temperature
            if (temp.average !== undefined) {
              if (!hashboard.temperature) {
                hashboard.temperature = {};
              }
              hashboard.temperature.latest = createMeasurement(temp.average, temp.unit as MetricUnit);
            }

            // Update inlet temp
            if (temp.inlet !== undefined) {
              if (!hashboard.inletTemp) {
                hashboard.inletTemp = {};
              }
              hashboard.inletTemp.latest = createMeasurement(temp.inlet, temp.unit as MetricUnit);
            }

            // Update outlet temp
            if (temp.outlet !== undefined) {
              if (!hashboard.outletTemp) {
                hashboard.outletTemp = {};
              }
              hashboard.outletTemp.latest = createMeasurement(temp.outlet, temp.unit as MetricUnit);
            }
          }

          // Update ASIC latest metrics (nested within hashboard)
          if (hashboardData.asics && hashboardData.serial_number) {
            const asicTelemetry = hashboardData.asics;

            // ASICs are returned as arrays of values indexed by ASIC position
            const numAsics = asicTelemetry.hashrate?.values?.length || asicTelemetry.temperature?.values?.length || 0;

            // Collect valid temperatures while updating ASICs (for avg/max computation)
            const temps: number[] = [];
            const tempUnit = asicTelemetry.temperature?.unit as MetricUnit | undefined;

            for (let asicIndex = 0; asicIndex < numAsics; asicIndex++) {
              const asicId = getAsicId(hashboardData.serial_number, asicIndex.toString());

              // Create or get ASIC
              if (!state.telemetry.asics.has(asicId)) {
                state.telemetry.asics.set(asicId, {
                  id: asicId,
                });
              }

              const asic = state.telemetry.asics.get(asicId)!;

              // Update temperature
              if (asicTelemetry.temperature?.values?.[asicIndex] !== undefined) {
                const tempValue = asicTelemetry.temperature.values[asicIndex];

                if (!asic.temperature) {
                  asic.temperature = {};
                }
                asic.temperature.latest = createMeasurement(tempValue, asicTelemetry.temperature.unit as MetricUnit);

                // Collect valid temperature for avg/max computation
                if (tempValue !== null && tempValue !== undefined) {
                  temps.push(tempValue);
                }
              }

              // Update hashrate
              if (asicTelemetry.hashrate?.values?.[asicIndex] !== undefined) {
                if (!asic.hashrate) {
                  asic.hashrate = {};
                }
                asic.hashrate.latest = createMeasurement(
                  asicTelemetry.hashrate.values[asicIndex],
                  asicTelemetry.hashrate.unit as MetricUnit,
                );
              }
            }

            // Compute avg/max ASIC temperatures for this hashboard
            if (temps.length > 0 && tempUnit) {
              const avgTemp = temps.reduce((sum, t) => sum + t, 0) / temps.length;
              const maxTemp = Math.max(...temps);

              if (!hashboard.avgAsicTemp) {
                hashboard.avgAsicTemp = {};
              }
              hashboard.avgAsicTemp.latest = createMeasurement(avgTemp, tempUnit);

              if (!hashboard.maxAsicTemp) {
                hashboard.maxAsicTemp = {};
              }
              hashboard.maxAsicTemp.latest = createMeasurement(maxTemp, tempUnit);
            }
          }
        });
      }

      // Update PSU latest metrics
      if (telemetryData.psus) {
        telemetryData.psus.forEach((psuData) => {
          try {
            // PSU index is 0-based in API, but we use 1-based IDs (slot numbers)
            const psuId = psuData.index + 1;

            // Validate required fields exist
            if (!psuData.voltage || !psuData.current || !psuData.power || !psuData.temperature) {
              console.warn(`PSU ${psuId} missing required telemetry fields`);
              return; // Skip this PSU
            }

            // Create or get PSU
            if (!state.telemetry.psus.has(psuId)) {
              state.telemetry.psus.set(psuId, { id: psuId });
            }

            const psu = state.telemetry.psus.get(psuId)!;

            // Update voltage metrics (voltage is required per API schema)
            if (psuData.voltage.input !== undefined && psuData.voltage.input !== null) {
              if (!psu.inputVoltage) psu.inputVoltage = {};
              psu.inputVoltage.latest = createMeasurement(psuData.voltage.input, psuData.voltage.unit as MetricUnit);
            }
            if (psuData.voltage.output !== undefined && psuData.voltage.output !== null) {
              if (!psu.outputVoltage) psu.outputVoltage = {};
              psu.outputVoltage.latest = createMeasurement(psuData.voltage.output, psuData.voltage.unit as MetricUnit);
            }

            // Update current metrics (current is required per API schema)
            if (psuData.current.input !== undefined && psuData.current.input !== null) {
              if (!psu.inputCurrent) psu.inputCurrent = {};
              psu.inputCurrent.latest = createMeasurement(psuData.current.input, psuData.current.unit as MetricUnit);
            }
            if (psuData.current.output !== undefined && psuData.current.output !== null) {
              if (!psu.outputCurrent) psu.outputCurrent = {};
              psu.outputCurrent.latest = createMeasurement(psuData.current.output, psuData.current.unit as MetricUnit);
            }

            // Update power metrics (power is required per API schema)
            if (psuData.power.input !== undefined && psuData.power.input !== null) {
              if (!psu.inputPower) psu.inputPower = {};
              psu.inputPower.latest = createMeasurement(psuData.power.input, psuData.power.unit as MetricUnit);
            }
            if (psuData.power.output !== undefined && psuData.power.output !== null) {
              if (!psu.outputPower) psu.outputPower = {};
              psu.outputPower.latest = createMeasurement(psuData.power.output, psuData.power.unit as MetricUnit);
            }

            // Update temperature metrics as individual properties (temperature is required per API schema)
            const tempUnit = psuData.temperature.unit as MetricUnit;

            if (psuData.temperature.ambient !== undefined && psuData.temperature.ambient !== null) {
              if (!psu.temperatureAmbient) psu.temperatureAmbient = {};
              psu.temperatureAmbient.latest = createMeasurement(psuData.temperature.ambient, tempUnit);
            }

            if (psuData.temperature.average !== undefined && psuData.temperature.average !== null) {
              if (!psu.temperatureAverage) psu.temperatureAverage = {};
              psu.temperatureAverage.latest = createMeasurement(psuData.temperature.average, tempUnit);
            }

            if (psuData.temperature.hotspot !== undefined && psuData.temperature.hotspot !== null) {
              if (!psu.temperatureHotspot) psu.temperatureHotspot = {};
              psu.temperatureHotspot.latest = createMeasurement(psuData.temperature.hotspot, tempUnit);
            }
          } catch (error) {
            console.error(`Failed to update PSU ${psuData.index + 1} telemetry:`, error);
          }
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
          const metric = state.telemetry.miner![key as keyof MinerTelemetryData];
          // Only clear if this is actually a MetricTimeSeries (has endTime property)
          if (metric && typeof metric === "object" && "endTime" in metric && typeof metric.endTime === "number") {
            if (metric.endTime < olderThanTimestamp) {
              // Type assertion is safe here because we've verified it's a MetricTimeSeries
              (state.telemetry.miner![key as keyof MinerTelemetryData] as MetricTimeSeries | undefined) = undefined;
            }
          }
        });
      }

      // Clear old hashboard data
      for (const [, hashboard] of state.telemetry.hashboards) {
        Object.keys(hashboard).forEach((key) => {
          const metric = hashboard[key as keyof HashboardTelemetryData];
          // Only clear if this is actually a MetricTimeSeries (has endTime property)
          if (metric && typeof metric === "object" && "endTime" in metric && typeof metric.endTime === "number") {
            if (metric.endTime < olderThanTimestamp) {
              (hashboard[key as keyof HashboardTelemetryData] as MetricTimeSeries | undefined) = undefined;
            }
          }
        });
      }

      // Clear old asic data
      for (const [, asic] of state.telemetry.asics) {
        Object.keys(asic).forEach((key) => {
          const metric = asic[key as keyof AsicTelemetryData];
          // Only clear if this is actually a MetricTimeSeries (has endTime property)
          if (metric && typeof metric === "object" && "endTime" in metric && typeof metric.endTime === "number") {
            if (metric.endTime < olderThanTimestamp) {
              (asic[key as keyof AsicTelemetryData] as MetricTimeSeries | undefined) = undefined;
            }
          }
        });
      }
    }),

  // Update optional hashboard temperature sensors
  updateHashboardTemperatures: (hashboardSerial, inletTemp, outletTemp, avgAsicTemp, maxAsicTemp) =>
    set((state) => {
      let hashboard = state.telemetry.hashboards.get(hashboardSerial);

      // Create hashboard if it doesn't exist
      if (!hashboard) {
        hashboard = {
          serial: hashboardSerial,
        };
        state.telemetry.hashboards.set(hashboardSerial, hashboard);
      }

      // Update temperature data - wrap Measurement in MetricTelemetry structure
      if (inletTemp) {
        hashboard.inletTemp = { latest: inletTemp };
      }
      if (outletTemp) {
        hashboard.outletTemp = { latest: outletTemp };
      }
      if (avgAsicTemp) {
        hashboard.avgAsicTemp = { latest: avgAsicTemp };
      }
      if (maxAsicTemp) {
        hashboard.maxAsicTemp = { latest: maxAsicTemp };
      }
    }),

  // PSU/Fan Update Actions
  updatePsuTelemetry: (psuId, telemetryData) =>
    set((state) => {
      let psu = state.telemetry.psus.get(psuId);

      // Create PSU if it doesn't exist
      if (!psu) {
        psu = { id: psuId };
        state.telemetry.psus.set(psuId, psu);
      }

      // Update telemetry data
      Object.entries(telemetryData).forEach(([key, value]) => {
        if (key !== "id" && value !== undefined) {
          (psu as any)[key] = value;
        }
      });
    }),

  updateFanTelemetry: (fanSlot, telemetryData) =>
    set((state) => {
      let fan = state.telemetry.fans.get(fanSlot);

      // Create Fan if it doesn't exist
      if (!fan) {
        fan = { slot: fanSlot };
        state.telemetry.fans.set(fanSlot, fan);
      }

      // Update telemetry data
      Object.entries(telemetryData).forEach(([key, value]) => {
        if (key !== "slot" && value !== undefined) {
          (fan as any)[key] = value;
        }
      });
    }),

  // Update cooling mode
  updateCoolingMode: (mode) =>
    set((state) => {
      state.telemetry.coolingMode = mode;
    }),

  // Clear all telemetry data (useful when duration changes)
  clearTimeSeriesData: () =>
    set((state) => {
      // Clear timeSeries from miner metrics, preserve latest
      if (state.telemetry.miner) {
        Object.keys(state.telemetry.miner).forEach((key) => {
          const metric = state.telemetry.miner![key as keyof MinerTelemetryData];
          if (metric && typeof metric === "object" && "timeSeries" in metric) {
            delete (metric as MetricTelemetry).timeSeries;
          }
        });
      }

      // Clear timeSeries from hashboard metrics, preserve latest
      for (const hashboard of state.telemetry.hashboards.values()) {
        Object.keys(hashboard).forEach((key) => {
          const metric = hashboard[key as keyof HashboardTelemetryData];
          if (metric && typeof metric === "object" && "timeSeries" in metric) {
            delete (metric as MetricTelemetry).timeSeries;
          }
        });
      }

      // Clear timeSeries from ASIC metrics, preserve latest
      for (const asic of state.telemetry.asics.values()) {
        Object.keys(asic).forEach((key) => {
          const metric = asic[key as keyof AsicTelemetryData];
          if (metric && typeof metric === "object" && "timeSeries" in metric) {
            delete (metric as MetricTelemetry).timeSeries;
          }
        });
      }
    }),

  clearLatestData: () =>
    set((state) => {
      // Clear latest from miner metrics, preserve timeSeries
      if (state.telemetry.miner) {
        Object.keys(state.telemetry.miner).forEach((key) => {
          const metric = state.telemetry.miner![key as keyof MinerTelemetryData];
          if (metric && typeof metric === "object" && "latest" in metric) {
            delete (metric as MetricTelemetry).latest;
          }
        });
      }

      // Clear latest from hashboard metrics, preserve timeSeries
      for (const hashboard of state.telemetry.hashboards.values()) {
        Object.keys(hashboard).forEach((key) => {
          const metric = hashboard[key as keyof HashboardTelemetryData];
          if (metric && typeof metric === "object" && "latest" in metric) {
            delete (metric as MetricTelemetry).latest;
          }
        });
      }

      // Clear latest from ASIC metrics, preserve timeSeries
      for (const asic of state.telemetry.asics.values()) {
        Object.keys(asic).forEach((key) => {
          const metric = asic[key as keyof AsicTelemetryData];
          if (metric && typeof metric === "object" && "latest" in metric) {
            delete (metric as MetricTelemetry).latest;
          }
        });
      }
    }),

  clearAllData: () =>
    set((state) => {
      state.telemetry.miner = null;
      state.telemetry.hashboards = new Map();
      state.telemetry.asics = new Map();
      state.telemetry.psus = new Map();
      state.telemetry.fans = new Map();
      state.telemetry.lastApiResponse = null;
      state.telemetry.lastUpdated = Date.now();
    }),
});
