import { useMemo } from "react";
import { useShallow } from "zustand/react/shallow";
import useMinerStore from "../useMinerStore";

// =============================================================================
// Hardware Convenience Hooks
// =============================================================================

// Miner hooks
export const useMinerHardware = () =>
  useMinerStore((state) => state.hardware.miner);

// Hashboard hooks
export const useHashboardsHardware = () => {
  const hashboards = useMinerStore((state) => state.hardware.hashboards);

  return useMemo(() => Array.from(hashboards.values()), [hashboards]);
};

export const useHashboardSerials = () =>
  useMinerStore(
    useShallow((state) =>
      Array.from(state.hardware.hashboards.values()).map((hb) => hb.serial),
    ),
  );

export const useHashboardHardware = (serial: string) =>
  useMinerStore((state) => state.hardware.getHashboard(serial));

export const useHashboardsByBay = (bay: number) =>
  useMinerStore((state) => state.hardware.getHashboardsByBay(bay));

export const useBayCount = () =>
  useMinerStore((state) => state.hardware.getBayCount());

export const useHashboardSlot = (serial: string) =>
  useMinerStore((state) => state.hardware.getSlotByHbSn(serial));

export const useHashboardBay = (serial: string) =>
  useMinerStore((state) => state.hardware.getBayByHbSn(serial));

export const useHashboardBaySlotIndex = (serial: string) =>
  useMinerStore((state) => state.hardware.getBaySlotIndexByHbSn(serial));

export const useAsicRowsByHbSn = (serial: string) => {
  const hashboard = useMinerStore((state) =>
    state.hardware.getHashboard(serial),
  );
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
export const useAsicHardware = (id: string) =>
  useMinerStore((state) => state.hardware.getAsic(id));

export const useAsicPosition = (id: string) =>
  useMinerStore((state) => state.hardware.getAsicPosition(id));

export const useAsicsByHashboard = (hashboardSerial: string) =>
  useMinerStore((state) => state.hardware.getAsicsByHashboard(hashboardSerial));

// Controlboard hooks
export const useControlBoard = () =>
  useMinerStore((state) => state.hardware.controlBoard);

// PSU hooks
export const usePsus = () =>
  useMinerStore((state) => state.hardware.getAllPsus());

export const usePsuIds = () =>
  useMinerStore(
    useShallow((state) =>
      Array.from(state.hardware.psus.values()).map((psu) => psu.id),
    ),
  );

export const usePsu = (id: number) =>
  useMinerStore((state) => state.hardware.getPsu(id));

// Fan hooks
export const useFans = () =>
  useMinerStore((state) => state.hardware.getAllFans());

export const useFanIds = () =>
  useMinerStore(
    useShallow((state) =>
      Array.from(state.hardware.fans.values()).map((fan) => fan.id),
    ),
  );

export const useFan = (id: number) =>
  useMinerStore((state) => state.hardware.getFan(id));
