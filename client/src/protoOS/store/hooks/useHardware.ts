import { useMemo } from "react";
import useMinerStore from "../useMinerStore";

// =============================================================================
// Hardware Convenience Hooks
// =============================================================================

// Miner hooks
export const useMinerHardware = () =>
  useMinerStore((state) => state.hardware.miner);

// Hashboard hooks
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
