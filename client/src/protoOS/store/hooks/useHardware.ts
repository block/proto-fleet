import { useMemo } from "react";
import { useShallow } from "zustand/react/shallow";
import useMinerStore from "../useMinerStore";

// TODO: This should come from the API when available
const SLOTS_PER_BAY = 3;

// =============================================================================
// Hardware Convenience Hooks
// =============================================================================

// Miner hooks
export const useMinerHardware = () => useMinerStore((state) => state.hardware.miner);

// Hashboard hooks
export const useHashboardsHardware = () => {
  const hashboards = useMinerStore((state) => state.hardware.hashboards);

  return useMemo(() => Array.from(hashboards.values()), [hashboards]);
};

export const useHashboardSerials = () =>
  useMinerStore(
    useShallow((state) =>
      Array.from(state.hardware.hashboards.values())
        .sort((a, b) => (a.slot ?? 0) - (b.slot ?? 0))
        .map((hb) => hb.serial),
    ),
  );

/**
 * Returns hashboard serials grouped by bay, with null for empty slots
 * @returns Object with bay indices as keys, arrays of serials (or null) as values
 * @example
 * // For a miner with 2 bays, 3 slots per bay, with one missing hashboard:
 * // Returns: { 1: ["serial1", "serial2", "serial3"], 2: ["serial4", null, "serial6"] }
 */
export const useHashboardSerialsByBay = () => {
  const hashboards = useMinerStore((state) => state.hardware.hashboards);
  const maxBayIndex = useMinerStore((state) => state.hardware.getBayCount());

  return useMemo(() => {
    const hashboardsArray = Array.from(hashboards.values());
    const hashboardsByBay: Record<number, (string | null)[]> = {};

    // Bay indices are 1-based, so iterate from 1 to maxBayIndex
    for (let bay = 1; bay <= maxBayIndex; bay++) {
      // Filter hashboards that belong to this bay
      // If bay property is not set, try to derive it from slot
      const hashboardsInBay = hashboardsArray.filter((hb) => {
        if (hb.bay !== undefined) {
          return hb.bay === bay;
        }
        // Fallback: calculate bay from slot if bay is not set
        // slot / SLOTS_PER_BAY gives us the bay (0-indexed), add 1 to make it 1-indexed
        if (hb.slot !== undefined) {
          const calculatedBay = Math.floor(hb.slot / SLOTS_PER_BAY) + 1;
          return calculatedBay === bay;
        }
        return false;
      });

      // Initialize array with nulls for all slots in the bay
      const serialsArray: (string | null)[] = Array(SLOTS_PER_BAY).fill(null);

      // Fill in the serials at their slot positions
      hashboardsInBay.forEach((hb) => {
        // Calculate slot index within the bay from slot number
        // Slots are 1-based, so subtract 1 to get 0-based index within the bay
        const slotIndex = hb.slot !== undefined ? (hb.slot - 1) % SLOTS_PER_BAY : 0;

        if (slotIndex < SLOTS_PER_BAY) {
          serialsArray[slotIndex] = hb.serial;
        }
      });

      hashboardsByBay[bay] = serialsArray;
    }

    return hashboardsByBay;
  }, [hashboards, maxBayIndex]);
};

export const useHashboardHardware = (serial: string) => useMinerStore((state) => state.hardware.getHashboard(serial));

export const useHashboardsByBay = (bay: number) => useMinerStore((state) => state.hardware.getHashboardsByBay(bay));

export const useSlotsPerBay = () => {
  // TODO: This should come from the API when available
  return SLOTS_PER_BAY;
};

export const useBayCount = () => useMinerStore((state) => state.hardware.getBayCount());

export const useHashboardSlot = (serial: string) => useMinerStore((state) => state.hardware.getSlotByHbSn(serial));

export const useHashboardBay = (serial: string) => useMinerStore((state) => state.hardware.getBayByHbSn(serial));

export const useAsicRowsByHbSn = (serial: string) => {
  const hashboard = useMinerStore((state) => state.hardware.getHashboard(serial));
  const allAsics = useMinerStore((state) => state.hardware.asics);

  return useMemo(() => {
    if (!hashboard || !hashboard.asicIds) return [];

    const asicRows = hashboard.asicIds
      .map((asicId) => allAsics.get(asicId)?.row)
      .filter((row): row is number => row !== undefined);

    const uniqueRows = new Set(asicRows);
    return Array.from(uniqueRows).sort((a, b) => a - b);
  }, [hashboard, allAsics]);
};

// ASIC hooks
export const useAsicHardware = (id: string) => useMinerStore((state) => state.hardware.getAsic(id));

export const useAsicPosition = (id: string) => useMinerStore((state) => state.hardware.getAsicPosition(id));

export const useAsicsByHashboard = (hashboardSerial: string) =>
  useMinerStore((state) => state.hardware.getAsicsByHashboard(hashboardSerial));

// Controlboard hooks
export const useControlBoard = () => useMinerStore((state) => state.hardware.controlBoard);

// PSU hooks
export const usePsus = () => useMinerStore((state) => state.hardware.getAllPsus());

export const usePsuIds = () =>
  useMinerStore(useShallow((state) => Array.from(state.hardware.psus.values()).map((psu) => psu.id)));

export const usePsu = (id: number) => useMinerStore((state) => state.hardware.getPsu(id));

// Fan hooks
export const useFans = () => useMinerStore((state) => state.hardware.getAllFans());

export const useFanIds = () =>
  useMinerStore(useShallow((state) => Array.from(state.hardware.fans.values()).map((fan) => fan.slot)));

export const useFan = (id: number) => useMinerStore((state) => state.hardware.getFan(id));
