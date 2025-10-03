import type { StateCreator } from "zustand";
import type {
  AsicHardwareData,
  AsicMap,
  HashboardHardwareData,
  HashboardMap,
  MinerHardwareData,
} from "../types";

// =============================================================================
// Hardware Slice Interface
// =============================================================================

export interface HardwareSlice {
  // State
  miner: MinerHardwareData | null;
  hashboards: HashboardMap;
  asics: AsicMap;

  // Miner Actions
  setMiner: (miner: MinerHardwareData) => void;
  getMiner: () => MinerHardwareData | null;

  // Hashboard Actions
  setHashboards: (hashboards: HashboardHardwareData[]) => void;
  addHashboard: (hashboard: HashboardHardwareData) => void;
  getHashboard: (serial: string) => HashboardHardwareData | undefined;
  getHashboardsByBay: (bay: number) => HashboardHardwareData[];
  getBayCount: () => number;
  getSlotByHbSn: (serial: string) => number | undefined;
  getBayByHbSn: (serial: string) => number | undefined;
  getBaySlotIndexByHbSn: (serial: string) => number | undefined;

  // ASIC Actions
  setAsics: (asics: AsicHardwareData[]) => void;
  addAsic: (asic: AsicHardwareData) => void;
  getAsic: (id: string) => AsicHardwareData | undefined;
  getAsicsByHashboard: (hashboardSerial: string) => AsicHardwareData[];
  getAsicPosition: (id: string) => { row: number; column: number } | undefined;
  getAsicRowsByHashboard: (hashboardSerial: string) => number[];

  // Relationship Actions
  linkAsicToHashboard: (asicId: string, hashboardSerial: string) => void;

  // Bulk Operations
  initializeMinerStructure: (
    miner: MinerHardwareData,
    hashboards: HashboardHardwareData[],
    asics: AsicHardwareData[],
  ) => void;
}

// =============================================================================
// Hardware Slice Implementation
// =============================================================================

export const createHardwareSlice: StateCreator<
  { hardware: HardwareSlice; telemetry: any; ui: any },
  [["zustand/immer", never]],
  [],
  HardwareSlice
> = (set, get) => ({
  // Initial state
  miner: null,
  hashboards: new Map(),
  asics: new Map(),

  // Miner Actions
  setMiner: (miner) =>
    set((state) => {
      state.hardware.miner = miner;
    }),

  getMiner: () => {
    const fullState = get() as {
      hardware: HardwareSlice;
      telemetry: any;
      ui: any;
    };
    return fullState.hardware.miner;
  },

  // Hashboard Actions
  setHashboards: (hashboards) =>
    set((state) => {
      state.hardware.hashboards.clear();
      hashboards.forEach((hb) => {
        state.hardware.hashboards.set(hb.serial, hb);
      });
    }),

  addHashboard: (hashboard) => {
    set((state) => {
      state.hardware.hashboards.set(hashboard.serial, hashboard);
    });

    // TODO: [STORE_REFACTOR] remove this when name is returened by the API
    // Update ASIC names if this hashboard has asicIds (call after state is updated)
    // if (hashboard.asicIds && hashboard.asicIds.length > 0) {
    //   get().hardware.updateAsicNames(hashboard.serial);
    // }
  },

  getHashboard: (serial) => {
    const fullState = get() as {
      hardware: HardwareSlice;
      telemetry: any;
      ui: any;
    };
    return fullState.hardware.hashboards.get(serial);
  },

  getHashboardsByBay: (bay) => {
    const fullState = get() as {
      hardware: HardwareSlice;
      telemetry: any;
      ui: any;
    };
    return Array.from(fullState.hardware.hashboards.values()).filter(
      (hb) => hb.bay === bay,
    );
  },

  getBayCount: () => {
    const fullState = get() as {
      hardware: HardwareSlice;
      telemetry: any;
      ui: any;
    };
    const hashboards = Array.from(fullState.hardware.hashboards.values());
    if (hashboards.length === 0) return 0;
    return Math.max(...hashboards.map((hb) => hb.bay || -1));
  },

  getSlotByHbSn: (serial) => {
    const fullState = get() as {
      hardware: HardwareSlice;
      telemetry: any;
      ui: any;
    };
    return fullState.hardware.hashboards.get(serial)?.slot;
  },

  getBayByHbSn: (serial) => {
    const fullState = get() as {
      hardware: HardwareSlice;
      telemetry: any;
      ui: any;
    };
    return fullState.hardware.hashboards.get(serial)?.bay;
  },

  // returns the index of of the slot within a given bay
  // e.g. slot 4 would be index 0 of bay 2
  getBaySlotIndexByHbSn: (serial) => {
    const fullState = get() as {
      hardware: HardwareSlice;
      telemetry: any;
      ui: any;
    };
    return fullState.hardware.hashboards.get(serial)?.slotIndexByBay;
  },

  // ASIC Actions
  setAsics: (asics) =>
    set((state) => {
      state.hardware.asics.clear();
      asics.forEach((asic) => {
        state.hardware.asics.set(asic.id, asic);
      });
    }),

  addAsic: (asic) =>
    set((state) => {
      state.hardware.asics.set(asic.id, asic);
    }),

  getAsic: (id) => {
    const fullState = get() as {
      hardware: HardwareSlice;
      telemetry: any;
      ui: any;
    };
    return fullState.hardware.asics.get(id);
  },

  getAsicsByHashboard: (hashboardSerial) => {
    const fullState = get() as {
      hardware: HardwareSlice;
      telemetry: any;
      ui: any;
    };
    return Array.from(fullState.hardware.asics.values()).filter(
      (asic) => asic.hashboardSerial === hashboardSerial,
    );
  },

  getAsicPosition: (id) => {
    const fullState = get() as {
      hardware: HardwareSlice;
      telemetry: any;
      ui: any;
    };
    const asic = fullState.hardware.asics.get(id);
    return asic ? { row: asic.row, column: asic.column } : undefined;
  },

  getAsicRowsByHashboard: (hashboardSerial) => {
    const fullState = get() as {
      hardware: HardwareSlice;
      telemetry: any;
      ui: any;
    };
    const hashboard = fullState.hardware.hashboards.get(hashboardSerial);
    if (!hashboard?.asicIds) return [];

    const asicRows = hashboard.asicIds
      .map((asicId) => fullState.hardware.asics.get(asicId)?.row)
      .filter((row): row is number => row !== undefined);

    const uniqueRows = new Set(asicRows);
    return Array.from(uniqueRows).sort((a, b) => a - b);
  },

  // Relationship Actions
  linkAsicToHashboard: (asicId, hashboardSerial) => {
    set((state) => {
      const hashboard = state.hardware.hashboards.get(hashboardSerial);
      if (hashboard?.asicIds && !hashboard.asicIds.includes(asicId)) {
        hashboard.asicIds.push(asicId);
      }
    });
  },

  // Bulk Operations
  initializeMinerStructure: (miner, hashboards, asics) =>
    set((state) => {
      state.hardware.miner = miner;

      // Initialize hashboards
      state.hardware.hashboards.clear();
      hashboards.forEach((hb) => {
        state.hardware.hashboards.set(hb.serial, hb);
      });

      // Initialize ASICs
      state.hardware.asics.clear();
      asics.forEach((asic) => {
        state.hardware.asics.set(asic.id, asic);
      });
    }),
});
