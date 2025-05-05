import { create } from "zustand";

export type HashboardMap = {
  [key: string]: {
    slot: number;
    bay: number;
    slotIndexByBay?: number;
  };
};

interface HashboardStore {
  mapping: HashboardMap;
  setMapping: (newMapping: HashboardMap) => void;
  getSlotByHbSn: (hbSn: string) => number | undefined;
  getBayByHbSn: (hbSn: string) => number | undefined;
  getBayCount: () => number;
  getBaySlotIndexByHbSn: (hbSn: string) => number | undefined;
}

const useHashboardLocationStore = create<HashboardStore>((set, get) => {
  let cachedBayCount = 0;
  let cachedMapping: HashboardMap = {};

  const calculateBayCount = () => {
    return Object.values(get().mapping).reduce((acc, { bay }) => {
      if (bay > acc) {
        acc = bay;
      }
      return acc;
    }, 0);
  };

  const calculateSlotIndexByBay = (mapping: HashboardMap) => {
    let slotIndexInBay = 0;
    let currentBay: number | undefined = undefined;

    // Sort the mapping by slot to ensure consistent ordering
    const sortedMapping = Object.entries(mapping).sort(
      ([, a], [, b]) => a.slot - b.slot,
    );

    for (const [serial, { bay }] of sortedMapping) {
      if (currentBay !== bay) {
        slotIndexInBay = 0;
      }

      currentBay = bay;
      mapping[serial].slotIndexByBay = slotIndexInBay;
      slotIndexInBay++;
    }
  };

  return {
    mapping: {},
    setMapping: (newMapping) => {
      calculateSlotIndexByBay(newMapping);
      set({ mapping: newMapping });
      cachedMapping = newMapping;
      cachedBayCount = calculateBayCount();
    },
    getSlotByHbSn: (hbSn) => get().mapping[hbSn]?.slot,
    getBayByHbSn: (hbSn) => get().mapping[hbSn]?.bay,
    getBayCount: () => {
      if (cachedMapping !== get().mapping) {
        cachedMapping = get().mapping;
        cachedBayCount = calculateBayCount();
      }
      return cachedBayCount;
    },
    getBaySlotIndexByHbSn: (hbSn) => {
      if (cachedMapping !== get().mapping) {
        cachedMapping = get().mapping;
        calculateSlotIndexByBay(cachedMapping);
      }
      return cachedMapping[hbSn]?.slotIndexByBay;
    },
  };
});

export default useHashboardLocationStore;
