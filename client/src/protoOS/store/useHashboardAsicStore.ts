import { enableMapSet } from "immer";
import { create } from "zustand";
import { subscribeWithSelector } from "zustand/middleware";
import { immer } from "zustand/middleware/immer";

import { Aggregates, AsicStats, TimeSeriesData } from "@/protoOS/api/types";

enableMapSet();

export interface AsicData extends AsicStats {
  hashboardSerial: string;
  // Historical data
  tempHistory: TimeSeriesData[];
  hashrateHistory: TimeSeriesData[];
  // Aggregates for historical data
  tempAggregates?: Aggregates;
  hashrateAggregates?: Aggregates;
  // Metadata
  lastUpdated?: number;
  lastHistoricalUpdate?: number;
}

export interface HashboardData {
  serial: string;
  asics: Map<number, AsicData>;
  lastUpdated?: number;
  powerUsageWatts?: number;
  avgAsicTempC?: number;
  inletTempC?: number;
  outletTempC?: number;
  hashrateGhs?: number;
}

export interface HistoricalData {
  tempHistory?: TimeSeriesData[];
  hashrateHistory?: TimeSeriesData[];
  tempAggregates?: Aggregates;
  hashrateAggregates?: Aggregates;
}

interface HashboardAsicStore {
  // State
  hashboards: Map<string, HashboardData>;

  // Actions
  setHashboard: (serial: string, hashboard: HashboardData) => void;
  updateAsicCurrentData: (
    hashboardSerial: string,
    asicId: number,
    data: { temp?: number; hashrate?: number },
  ) => void;
  updateAsicHistoricalData: (
    hashboardSerial: string,
    asicId: number,
    data: HistoricalData,
  ) => void;
  updateCompleteAsicData: (
    hashboardSerial: string,
    asicId: number,
    asicStats: AsicStats,
  ) => void;
  initializeAsic: (
    hashboardSerial: string,
    asicId: number,
    initialData?: Partial<AsicData>,
  ) => void;

  // Ergonomic batch operations
  updateMultipleAsics: (
    hashboardSerial: string,
    updates: Array<{
      asicId: number;
      currentData?: { temp?: number; hashrate?: number };
      historicalData?: {
        tempHistory?: TimeSeriesData[];
        hashrateHistory?: TimeSeriesData[];
        tempAggregates?: Aggregates;
        hashrateAggregates?: Aggregates;
      };
    }>,
  ) => void;

  // Convenient single-property updates
  updateAsicTemp: (
    hashboardSerial: string,
    asicId: number,
    temp?: number,
  ) => void;
  updateAsicHashrate: (
    hashboardSerial: string,
    asicId: number,
    hashrate?: number,
  ) => void;
  updatePowerUsage: (hashboardSerial: string, powerUsage?: number) => void;
  updateAvgAsicTemp: (hashboardSerial: string, avgTemp?: number) => void;
  updateInletTemp: (hashboardSerial: string, inletTemp?: number) => void;
  updateOutletTemp: (hashboardSerial: string, outletTemp?: number) => void;
  updateBoardHashrate: (hashboardSerial: string, avgHashrate?: number) => void;

  // Bulk initialization
  initializeHashboardAsics: (
    hashboardSerial: string,
    asicIds: number[],
    initialData?: Partial<AsicData>,
  ) => void;

  // Getters
  getHashboard: (serial: string) => HashboardData | undefined;
  getAsic: (hashboardSerial: string, asicId: number) => AsicData | undefined;
  getAllAsics: (hashboardSerial: string) => AsicData[];
  getAsicIds: (hashboardSerial: string) => number[];

  // Utility
  clearHashboard: (serial: string) => void;
  clearAllHashboards: () => void;
}

const useHashboardAsicStore = create<HashboardAsicStore>()(
  subscribeWithSelector(
    immer((set, get) => {
      // Helper functions for common "get or create" patterns
      const getOrCreateHashboard = (
        state: any,
        hashboardSerial: string,
      ): HashboardData => {
        let hashboard = state.hashboards.get(hashboardSerial);
        if (!hashboard) {
          hashboard = {
            serial: hashboardSerial,
            asics: new Map(),
            lastUpdated: Date.now(),
          };
          state.hashboards.set(hashboardSerial, hashboard);
        }
        return hashboard;
      };

      const getOrCreateAsic = (
        hashboard: HashboardData,
        asicId: number,
      ): AsicData => {
        let asic = hashboard.asics.get(asicId);
        if (!asic) {
          asic = {
            id: asicId,
            hashboardSerial: hashboard.serial,
            tempHistory: [],
            hashrateHistory: [],
            lastUpdated: Date.now(),
          } as AsicData;
          hashboard.asics.set(asicId, asic);
        }
        return asic;
      };

      const updateHashboardTimestamp = (hashboard: HashboardData) => {
        hashboard.lastUpdated = Date.now();
      };

      const updateAsicTimestamp = (asic: AsicData) => {
        asic.lastUpdated = Date.now();
      };

      const updateAsicHistoricalTimestamp = (asic: AsicData) => {
        asic.lastHistoricalUpdate = Date.now();
      };

      return {
        // Initial state
        hashboards: new Map(),

        // Actions
        setHashboard: (serial, hashboard) => {
          set((state) => {
            state.hashboards.set(serial, {
              ...hashboard,
              lastUpdated: Date.now(),
            });
          });
        },

        updateAsicCurrentData: (hashboardSerial, asicId, data) => {
          set((state) => {
            const hashboard = getOrCreateHashboard(state, hashboardSerial);
            const asic = getOrCreateAsic(hashboard, asicId);

            // Update ASIC data using AsicStats fields
            if (data.temp !== undefined) asic.temp_c = data.temp;
            if (data.hashrate !== undefined) asic.hashrate_ghs = data.hashrate;
            updateAsicTimestamp(asic);
            updateHashboardTimestamp(hashboard);
          });
        },

        updateAsicHistoricalData: (hashboardSerial, asicId, data) => {
          set((state) => {
            const hashboard = getOrCreateHashboard(state, hashboardSerial);
            const asic = getOrCreateAsic(hashboard, asicId);

            // Update historical data
            if (data.tempHistory) asic.tempHistory = data.tempHistory;
            if (data.hashrateHistory)
              asic.hashrateHistory = data.hashrateHistory;
            if (data.tempAggregates) asic.tempAggregates = data.tempAggregates;
            if (data.hashrateAggregates)
              asic.hashrateAggregates = data.hashrateAggregates;
            updateAsicHistoricalTimestamp(asic);
            updateHashboardTimestamp(hashboard);
          });
        },

        updateCompleteAsicData: (hashboardSerial, asicId, asicStats) => {
          set((state) => {
            const hashboard = getOrCreateHashboard(state, hashboardSerial);
            const existingAsic = hashboard.asics.get(asicId);

            // Preserve historical data and metadata if ASIC already exists
            const preservedData = existingAsic
              ? {
                  tempHistory: existingAsic.tempHistory,
                  hashrateHistory: existingAsic.hashrateHistory,
                  tempAggregates: existingAsic.tempAggregates,
                  hashrateAggregates: existingAsic.hashrateAggregates,
                  lastHistoricalUpdate: existingAsic.lastHistoricalUpdate,
                }
              : {
                  tempHistory: [],
                  hashrateHistory: [],
                };

            // Create complete ASIC data with all AsicStats fields
            const completeAsicData: AsicData = {
              ...asicStats,
              hashboardSerial,
              ...preservedData,
              lastUpdated: Date.now(),
            };

            hashboard.asics.set(asicId, completeAsicData);
            updateHashboardTimestamp(hashboard);
          });
        },

        initializeAsic: (hashboardSerial, asicId, initialData) => {
          set((state) => {
            const hashboard = getOrCreateHashboard(state, hashboardSerial);

            // Only initialize if ASIC doesn't exist
            if (!hashboard.asics.has(asicId)) {
              const newAsic: AsicData = {
                id: asicId,
                hashboardSerial,
                tempHistory: [],
                hashrateHistory: [],
                lastUpdated: Date.now(),
                ...initialData,
              } as AsicData;
              hashboard.asics.set(asicId, newAsic);
              updateHashboardTimestamp(hashboard);
            }
          });
        },

        // Ergonomic batch operations
        updateMultipleAsics: (hashboardSerial, updates) => {
          set((state) => {
            const hashboard = getOrCreateHashboard(state, hashboardSerial);

            // Process all updates
            for (const update of updates) {
              const asic = getOrCreateAsic(hashboard, update.asicId);

              // Update current data using AsicStats fields
              if (update.currentData) {
                if (update.currentData.temp !== undefined) {
                  asic.temp_c = update.currentData.temp;
                }
                if (update.currentData.hashrate !== undefined) {
                  asic.hashrate_ghs = update.currentData.hashrate;
                }
                updateAsicTimestamp(asic);
              }

              // Update historical data
              if (update.historicalData) {
                if (update.historicalData.tempHistory) {
                  asic.tempHistory = update.historicalData.tempHistory;
                }
                if (update.historicalData.hashrateHistory) {
                  asic.hashrateHistory = update.historicalData.hashrateHistory;
                }
                if (update.historicalData.tempAggregates) {
                  asic.tempAggregates = update.historicalData.tempAggregates;
                }
                if (update.historicalData.hashrateAggregates) {
                  asic.hashrateAggregates =
                    update.historicalData.hashrateAggregates;
                }
                updateAsicHistoricalTimestamp(asic);
              }
            }

            updateHashboardTimestamp(hashboard);
          });
        },

        // Convenient single-property updates
        updateAsicTemp: (hashboardSerial, asicId, temp) => {
          if (temp === undefined) return;

          set((state) => {
            const hashboard = state.hashboards.get(hashboardSerial);
            const asic = hashboard?.asics.get(asicId);
            if (asic && hashboard) {
              asic.temp_c = temp;
              updateAsicTimestamp(asic);
              updateHashboardTimestamp(hashboard);
            }
          });
        },

        updateAsicHashrate: (hashboardSerial, asicId, hashrate) => {
          if (hashrate === undefined) return;

          set((state) => {
            const hashboard = state.hashboards.get(hashboardSerial);
            const asic = hashboard?.asics.get(asicId);
            if (asic && hashboard) {
              asic.hashrate_ghs = hashrate;
              updateAsicTimestamp(asic);
              updateHashboardTimestamp(hashboard);
            }
          });
        },

        updateAvgAsicTemp: (hashboardSerial, avgTemp) => {
          if (avgTemp === undefined) return;

          set((state) => {
            const hashboard = state.hashboards.get(hashboardSerial);
            if (hashboard) {
              hashboard.avgAsicTempC = avgTemp;
              updateHashboardTimestamp(hashboard);
            }
          });
        },

        updateInletTemp: (hashboardSerial, inletTemp) => {
          if (inletTemp === undefined) return;

          set((state) => {
            const hashboard = state.hashboards.get(hashboardSerial);
            if (hashboard) {
              hashboard.inletTempC = inletTemp;
              updateHashboardTimestamp(hashboard);
            }
          });
        },

        updateOutletTemp: (hashboardSerial, outletTemp) => {
          if (outletTemp === undefined) return;

          set((state) => {
            const hashboard = state.hashboards.get(hashboardSerial);
            if (hashboard) {
              hashboard.outletTempC = outletTemp;
              updateHashboardTimestamp(hashboard);
            }
          });
        },

        updatePowerUsage: (hashboardSerial, powerUsage) => {
          if (powerUsage === undefined) return;
          set((state) => {
            const hashboard = state.hashboards.get(hashboardSerial);
            if (hashboard) {
              hashboard.powerUsageWatts = powerUsage;
              updateHashboardTimestamp(hashboard);
            }
          });
        },

        updateBoardHashrate: (hashboardSerial, hashrate) => {
          if (hashrate === undefined) return;

          set((state) => {
            const hashboard = state.hashboards.get(hashboardSerial);
            if (hashboard) {
              hashboard.hashrateGhs = hashrate;
              updateHashboardTimestamp(hashboard);
            }
          });
        },

        // Bulk initialization
        initializeHashboardAsics: (hashboardSerial, asicIds, initialData) => {
          set((state) => {
            const hashboard = getOrCreateHashboard(state, hashboardSerial);

            // Initialize all ASICs that don't exist
            let hasNewAsics = false;
            for (const asicId of asicIds) {
              if (!hashboard.asics.has(asicId)) {
                const newAsic: AsicData = {
                  id: asicId,
                  hashboardSerial,
                  tempHistory: [],
                  hashrateHistory: [],
                  lastUpdated: Date.now(),
                  ...initialData,
                } as AsicData;
                hashboard.asics.set(asicId, newAsic);
                hasNewAsics = true;
              }
            }

            if (hasNewAsics) {
              updateHashboardTimestamp(hashboard);
            }
          });
        },

        // Getters (these don't need Immer since they don't mutate)
        getHashboard: (serial) => {
          return get().hashboards.get(serial);
        },

        getAsic: (hashboardSerial, asicId) => {
          const hashboard = get().hashboards.get(hashboardSerial);
          return hashboard?.asics.get(asicId);
        },

        getAllAsics: (hashboardSerial) => {
          const hashboard = get().hashboards.get(hashboardSerial);
          return hashboard ? Array.from(hashboard.asics.values()) : [];
        },

        getAsicIds: (hashboardSerial) => {
          const hashboard = get().hashboards.get(hashboardSerial);
          return hashboard ? Array.from(hashboard.asics.keys()) : [];
        },

        // Utility
        clearHashboard: (serial) => {
          set((state) => {
            state.hashboards.delete(serial);
          });
        },

        clearAllHashboards: () => {
          set((state) => {
            state.hashboards.clear();
          });
        },
      };
    }),
  ),
);

export default useHashboardAsicStore;
