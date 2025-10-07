import { useCallback, useEffect, useMemo, useState } from "react";

import { usePoll } from "./usePoll";
import { HashboardStatsHashboardstats } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { getAsicId } from "@/protoOS/store";
import { useMinerStore } from "@/protoOS/store";
interface UseHashboardStatusProps {
  hashboardSerialNumbers: string[];
  poll?: boolean;
}

// TODO: [STORE_REFACTOR] We only use this hook to fill in gaps that our useHardware doesnt currently provide
// - hashboard.asicIds
// - asic rows and columns
const useHashboardStatus = ({
  hashboardSerialNumbers,
  poll,
}: UseHashboardStatusProps) => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<
    Record<string, HashboardStatsHashboardstats>
  >({});
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const fetchData = useCallback(async () => {
    if (!api || hashboardSerialNumbers.length === 0) return;

    setPending(true);
    setError(undefined);

    try {
      const results = await Promise.all(
        hashboardSerialNumbers.map(async (serial) => {
          const res = await api.getHashboardStatus({ hbSn: serial });
          return { serial, data: res?.data["hashboard-stats"] };
        }),
      );

      const newData: Record<string, HashboardStatsHashboardstats> = {};
      results.forEach(({ serial, data }) => {
        if (data) {
          newData[serial] = data;
        }
      });

      setData(newData);
    } catch (err) {
      const errorMessage =
        err instanceof Error ? err.message : "Unknown error occurred";
      setError(errorMessage);
    } finally {
      setPending(false);
    }
  }, [hashboardSerialNumbers, api]);

  usePoll({
    fetchData,
    params: hashboardSerialNumbers,
    poll,
  });

  useEffect(() => {
    if (Object.keys(data).length === 0) return;

    // Process each hashboard's data
    Object.entries(data).forEach(([hashboardSerialNumber, hashboardData]) => {
      const asics = hashboardData?.asics;
      if (!asics || asics.length === 0) {
        return;
      }

      // Initialize hardware store with hashboard and ASIC structure
      const existingHashboard = useMinerStore
        .getState()
        .hardware.getHashboard(hashboardSerialNumber);

      const asicIds = asics
        .filter((asic) => asic?.id !== undefined)
        .map((asic) => getAsicId(hashboardSerialNumber, asic.id!));

      if (!existingHashboard) {
        useMinerStore.getState().hardware.addHashboard({
          serial: hashboardSerialNumber,
          asicIds,
        });
      } else {
        // Update existing hashboard with asicIds
        useMinerStore.getState().hardware.addHashboard({
          ...existingHashboard,
          asicIds,
        });
      }

      // Add ASIC info with positional data
      for (const asic of asics) {
        if (
          asic !== undefined &&
          asic.id !== undefined &&
          asic.row !== undefined &&
          asic.column !== undefined
        ) {
          // Create globally unique ASIC ID using consistent utility
          const asicId = getAsicId(hashboardSerialNumber, asic.id);
          const existingAsic = useMinerStore
            .getState()
            .hardware.getAsic(asicId);

          if (!existingAsic) {
            const asicInfo = {
              id: asicId,
              hashboardSerial: hashboardSerialNumber,
              row: asic.row,
              column: asic.column,
            };

            useMinerStore.getState().hardware.addAsic(asicInfo);
            useMinerStore
              .getState()
              .hardware.linkAsicToHashboard(asicId, hashboardSerialNumber);
          }
        }
      }
    });
  }, [data]);

  return useMemo(() => ({ pending, error, data }), [pending, error, data]);
};

export { useHashboardStatus };
