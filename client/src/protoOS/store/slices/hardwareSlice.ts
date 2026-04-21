import type { StateCreator } from "zustand";
import type {
  AsicHardwareData,
  AsicMap,
  ControlBoardHardwareData,
  FanHardwareData,
  FanMap,
  HashboardHardwareData,
  HashboardMap,
  MinerHardwareData,
  PsuHardwareData,
  PsuMap,
} from "../types";
import type { MinerStore } from "../useMinerStore";

// =============================================================================
// Hardware Slice Interface
// =============================================================================

export interface HardwareSlice {
  // State
  miner: MinerHardwareData | null;
  controlBoard: ControlBoardHardwareData | null;
  hashboards: HashboardMap;
  asics: AsicMap;
  psus: PsuMap;
  fans: FanMap;

  // Miner Actions
  setMiner: (miner: MinerHardwareData) => void;
  getMiner: () => MinerHardwareData | null;

  // Control Board Actions
  setControlBoard: (controlBoard: ControlBoardHardwareData) => void;
  getControlBoard: () => ControlBoardHardwareData | null;

  // Hashboard Actions
  setHashboards: (hashboards: HashboardHardwareData[]) => void;
  addHashboard: (hashboard: HashboardHardwareData) => void;
  getHashboard: (serial: string) => HashboardHardwareData | undefined;
  getHashboardBySlot: (slot: number) => HashboardHardwareData | undefined;
  getHashboardsByBay: (bay: number) => HashboardHardwareData[];
  getBayCount: () => number;
  getSlotByHbSn: (serial: string) => number | undefined;
  getBayByHbSn: (serial: string) => number | undefined;

  // ASIC Actions
  setAsics: (asics: AsicHardwareData[]) => void;
  addAsic: (asic: AsicHardwareData) => void;
  batchAddAsics: (asics: AsicHardwareData[]) => void;
  getAsic: (id: string) => AsicHardwareData | undefined;
  getAsicsByHashboard: (hashboardSerial: string) => AsicHardwareData[];
  getAsicPosition: (id: string) => { row?: number; column?: number } | undefined;
  getAsicRowsByHashboard: (hashboardSerial: string) => number[];

  // Relationship Actions
  linkAsicToHashboard: (asicId: string, hashboardSerial: string) => void;

  // PSU Actions
  setPsus: (psus: PsuHardwareData[]) => void;
  addPsu: (psu: PsuHardwareData) => void;
  getPsu: (id: number) => PsuHardwareData | undefined;
  getAllPsus: () => PsuHardwareData[];

  // Fan Actions
  setFans: (fans: FanHardwareData[]) => void;
  addFan: (fan: FanHardwareData) => void;
  getFan: (slot: number) => FanHardwareData | undefined;
  getAllFans: () => FanHardwareData[];

  // Bulk Operations
  initializeMinerStructure: (
    miner: MinerHardwareData,
    hashboards: HashboardHardwareData[],
    asics: AsicHardwareData[],
    psus?: PsuHardwareData[],
    fans?: FanHardwareData[],
    controlBoard?: ControlBoardHardwareData,
  ) => void;
}

// =============================================================================
// Hardware Slice Implementation
// =============================================================================

export const createHardwareSlice: StateCreator<MinerStore, [["zustand/immer", never]], [], HardwareSlice> = (
  set,
  get,
) => ({
  // Initial state
  miner: null,
  controlBoard: null,
  hashboards: new Map(),
  asics: new Map(),
  psus: new Map(),
  fans: new Map(),

  // Miner Actions
  setMiner: (miner) =>
    set((state) => {
      state.hardware.miner = miner;
    }),

  getMiner: () => {
    return get().hardware.miner;
  },

  // Control Board Actions
  setControlBoard: (controlBoard) =>
    set((state) => {
      state.hardware.controlBoard = controlBoard;
    }),

  getControlBoard: () => {
    return get().hardware.controlBoard;
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
    return get().hardware.hashboards.get(serial);
  },

  getHashboardBySlot: (slot) => {
    return Array.from(get().hardware.hashboards.values()).find((hb) => hb.slot === slot);
  },

  getHashboardsByBay: (bay) => {
    return Array.from(get().hardware.hashboards.values()).filter((hb) => hb.bay === bay);
  },

  getBayCount: () => {
    const hashboards = Array.from(get().hardware.hashboards.values());
    if (hashboards.length === 0) return 0;
    return Math.max(...hashboards.map((hb) => hb.bay || -1));
  },

  getSlotByHbSn: (serial) => {
    return get().hardware.hashboards.get(serial)?.slot;
  },

  getBayByHbSn: (serial) => {
    return get().hardware.hashboards.get(serial)?.bay;
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

  batchAddAsics: (asics) =>
    set((state) => {
      asics.forEach((asic) => {
        state.hardware.asics.set(asic.id, asic);
      });
    }),

  getAsic: (id) => {
    return get().hardware.asics.get(id);
  },

  getAsicsByHashboard: (hashboardSerial) => {
    return Array.from(get().hardware.asics.values()).filter((asic) => asic.hashboardSerial === hashboardSerial);
  },

  getAsicPosition: (id) => {
    const asic = get().hardware.asics.get(id);
    return asic ? { row: asic.row, column: asic.column } : undefined;
  },

  getAsicRowsByHashboard: (hashboardSerial) => {
    const state = get();
    const hashboard = state.hardware.hashboards.get(hashboardSerial);
    if (!hashboard?.asicIds) return [];

    const asicRows = hashboard.asicIds
      .map((asicId) => state.hardware.asics.get(asicId)?.row)
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

  // PSU Actions
  setPsus: (psus) =>
    set((state) => {
      state.hardware.psus.clear();
      psus.forEach((psu) => {
        state.hardware.psus.set(psu.id, psu);
      });
    }),

  addPsu: (psu) =>
    set((state) => {
      state.hardware.psus.set(psu.id, psu);
    }),

  getPsu: (id) => {
    return get().hardware.psus.get(id);
  },

  getAllPsus: () => {
    return Array.from(get().hardware.psus.values());
  },

  // Fan Actions
  setFans: (fans) =>
    set((state) => {
      state.hardware.fans.clear();
      fans.forEach((fan) => {
        state.hardware.fans.set(fan.slot, fan);
      });
    }),

  addFan: (fan) =>
    set((state) => {
      state.hardware.fans.set(fan.slot, fan);
    }),

  getFan: (slot) => {
    return get().hardware.fans.get(slot);
  },

  getAllFans: () => {
    return Array.from(get().hardware.fans.values());
  },

  // Bulk Operations
  initializeMinerStructure: (miner, hashboards, asics, psus, fans, controlBoard) =>
    set((state) => {
      state.hardware.miner = miner;

      // Initialize control board
      if (controlBoard) {
        state.hardware.controlBoard = controlBoard;
      }

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

      // Initialize PSUs
      if (psus) {
        state.hardware.psus.clear();
        psus.forEach((psu) => {
          state.hardware.psus.set(psu.id, psu);
        });
      }

      // Initialize Fans
      if (fans) {
        state.hardware.fans.clear();
        fans.forEach((fan) => {
          state.hardware.fans.set(fan.slot, fan);
        });
      }
    }),
});
