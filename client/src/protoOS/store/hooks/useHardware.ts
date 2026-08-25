import { useMemo } from "react";
import { useShallow } from "zustand/react/shallow";
import { asAsicId } from "../contract";
import type { AsicHardwareData, FanHardwareData, HashboardHardwareData, PsuHardwareData } from "../types";
import useMinerStore from "../useMinerStore";

// TODO: This should come from the API when available
const SLOTS_PER_BAY = 3;

// Stable empty results so hooks return the same reference while their component
// group is unpopulated (avoids churning downstream memos during loading).
const NO_PSUS: PsuHardwareData[] = [];
const NO_FANS: FanHardwareData[] = [];
const NO_ASICS: AsicHardwareData[] = [];
const NO_HASHBOARDS: HashboardHardwareData[] = [];

// =============================================================================
// Hardware Convenience Hooks
// =============================================================================

// Miner hooks
export const useMinerHardware = () => {
  const hardware = useMinerStore((state) => state.hardware);
  return useMemo(() => hardware.getMiner(), [hardware]);
};

// Hashboard/ASIC hardware is rewritten on every telemetry/hashboard-status poll.
// Subscribe to the specific sub-maps (hashboards + topology, or asics) rather than
// the whole `state.hardware` object, so an unrelated per-poll write to a different
// sub-map (immer reparents `state.hardware` on any nested change) doesn't re-render
// these hooks. getHashboard draws identity/firmware from `hashboards` and
// slot/bay/asicIds from `topology`, so both are dependencies.

// Hashboard hooks
export const useHashboardsHardware = () => {
  const hashboards = useMinerStore((state) => state.hardware.hashboards);
  const topology = useMinerStore((state) => state.hardware.topology);
  return useMemo(() => {
    if (hashboards.size === 0 && !topology) return NO_HASHBOARDS;
    const hardware = useMinerStore.getState().hardware;
    return [...hashboards.values()]
      .map((board) => hardware.getHashboard(board.identity.value))
      .filter((board) => board !== undefined);
  }, [hashboards, topology]);
};

export const useHashboardSerials = () =>
  useMinerStore(
    useShallow((state) =>
      [...state.hardware.hashboards.values()]
        .map((board) => state.hardware.getHashboard(board.identity.value))
        .filter((board) => board !== undefined)
        .sort((a, b) => (a.slot ?? 0) - (b.slot ?? 0))
        .map((board) => board.serial),
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
  const hashboards = useHashboardsHardware();
  const maxBayIndex = useMinerStore((state) => state.hardware.getBayCount());

  return useMemo(() => {
    const hashboardsArray = hashboards;
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
        // Convert the 1-based slot to a 0-based offset before deriving the 1-based bay.
        if (hb.slot !== undefined) {
          const calculatedBay = Math.floor((hb.slot - 1) / SLOTS_PER_BAY) + 1;
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

export const useHashboardHardware = (serial: string) => {
  const hashboards = useMinerStore((state) => state.hardware.hashboards);
  const topology = useMinerStore((state) => state.hardware.topology);
  return useMemo(() => {
    if (hashboards.size === 0 && !topology) return undefined;
    return useMinerStore.getState().hardware.getHashboard(serial);
  }, [hashboards, topology, serial]);
};

export const useHashboardsByBay = (bay: number) => {
  const hashboards = useMinerStore((state) => state.hardware.hashboards);
  const topology = useMinerStore((state) => state.hardware.topology);
  return useMemo(() => {
    if (hashboards.size === 0 && !topology) return NO_HASHBOARDS;
    return useMinerStore.getState().hardware.getHashboardsByBay(bay);
  }, [bay, hashboards, topology]);
};

export const useSlotsPerBay = () => {
  // TODO: This should come from the API when available
  return SLOTS_PER_BAY;
};

export const useBayCount = () => useMinerStore((state) => state.hardware.getBayCount());

export const useHashboardSlot = (serial: string) => useMinerStore((state) => state.hardware.getSlotByHbSn(serial));

export const useHashboardBay = (serial: string) => useMinerStore((state) => state.hardware.getBayByHbSn(serial));

export const useAsicRowsByHbSn = (serial: string) => {
  const asics = useMinerStore((state) => state.hardware.asics);
  const hashboards = useMinerStore((state) => state.hardware.hashboards);
  const topology = useMinerStore((state) => state.hardware.topology);

  return useMemo(() => {
    if (hashboards.size === 0 && !topology) return [];
    const hashboard = useMinerStore.getState().hardware.getHashboard(serial);
    if (!hashboard || !hashboard.asicIds) return [];

    const asicRows = hashboard.asicIds
      .map((asicId) => asics.get(asAsicId(asicId))?.row)
      .filter((row): row is number => row !== undefined);

    const uniqueRows = new Set(asicRows);
    return Array.from(uniqueRows).sort((a, b) => a - b);
  }, [asics, hashboards, topology, serial]);
};

// ASIC hooks — ASIC telemetry/positions live only in `state.hardware.asics`.
export const useAsicHardware = (id: string) => {
  const asics = useMinerStore((state) => state.hardware.asics);
  return useMemo(() => (asics.size === 0 ? undefined : useMinerStore.getState().hardware.getAsic(id)), [asics, id]);
};

export const useAsicPosition = (id: string) => {
  const asics = useMinerStore((state) => state.hardware.asics);
  return useMemo(
    () => (asics.size === 0 ? undefined : useMinerStore.getState().hardware.getAsicPosition(id)),
    [asics, id],
  );
};

export const useAsicsByHashboard = (hashboardSerial: string) => {
  const asics = useMinerStore((state) => state.hardware.asics);
  return useMemo(
    () => (asics.size === 0 ? NO_ASICS : useMinerStore.getState().hardware.getAsicsByHashboard(hashboardSerial)),
    [asics, hashboardSerial],
  );
};

// Controlboard, PSU, and fan hardware is static and — unlike hashboards/ASICs —
// is not rewritten on every telemetry poll. Subscribe to the specific sub-map so
// unrelated hardware writes (per-poll hashboard/ASIC batches) don't re-render
// these cards. The `size === 0` guard both references the subscribed map (so the
// getState() read stays honest to react-hooks/exhaustive-deps) and returns a
// stable empty while the group is unpopulated.

// Controlboard hooks
export const useControlBoard = () => {
  const controlBoards = useMinerStore((state) => state.hardware.controlBoards);
  return useMemo(
    () => (controlBoards.size === 0 ? null : useMinerStore.getState().hardware.getControlBoard()),
    [controlBoards],
  );
};

// PSU hooks
export const usePsus = () => {
  const psus = useMinerStore((state) => state.hardware.psus);
  return useMemo(() => (psus.size === 0 ? NO_PSUS : useMinerStore.getState().hardware.getAllPsus()), [psus]);
};

export const usePsuIds = () => useMinerStore(useShallow((state) => state.hardware.getAllPsus().map((psu) => psu.id)));

export const usePsu = (id: number) => {
  const psus = useMinerStore((state) => state.hardware.psus);
  return useMemo(() => (psus.size === 0 ? undefined : useMinerStore.getState().hardware.getPsu(id)), [psus, id]);
};

// Fan hooks
export const useFans = () => {
  const fans = useMinerStore((state) => state.hardware.fans);
  return useMemo(() => (fans.size === 0 ? NO_FANS : useMinerStore.getState().hardware.getAllFans()), [fans]);
};

export const useFanIds = () => useMinerStore(useShallow((state) => state.hardware.getAllFans().map((fan) => fan.slot)));

export const useFan = (id: number) => {
  const fans = useMinerStore((state) => state.hardware.fans);
  return useMemo(() => (fans.size === 0 ? undefined : useMinerStore.getState().hardware.getFan(id)), [fans, id]);
};
