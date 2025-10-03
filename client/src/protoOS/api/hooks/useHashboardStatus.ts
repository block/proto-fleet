import { useCallback, useEffect, useMemo, useState } from "react";

import { usePoll } from "./usePoll";
import { HashboardStatsHashboardstats } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { getAsicId } from "@/protoOS/store";
import { useMinerStore } from "@/protoOS/store";
interface UseHashboardStatusProps {
  hashboardSerialNumber: string;
  poll?: boolean;
}

const useHashboardStatus = ({
  hashboardSerialNumber,
  poll,
}: UseHashboardStatusProps) => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<HashboardStatsHashboardstats>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const fetchData = useCallback(() => {
    if (!api) return;

    setPending(true);
    api
      .getHashboardStatus({ hbSn: hashboardSerialNumber })
      .then((res) => {
        setData(res?.data["hashboard-stats"]);
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [hashboardSerialNumber, api]);

  usePoll({
    fetchData,
    params: hashboardSerialNumber,
    poll,
  });

  useEffect(() => {
    if (data) {
      const asics = data?.asics;
      if (!asics || asics.length === 0) {
        return;
      }

      // Initialize hardware store with hashboard and ASIC structure
      // Check if hashboard already exists to avoid overwriting
      const existingHashboard = useMinerStore
        .getState()
        .hardware.getHashboard(hashboardSerialNumber);
      if (!existingHashboard) {
        // Add hashboard info - get board ID from API response
        useMinerStore.getState().hardware.addHashboard({
          serial: hashboardSerialNumber,
          asicIds: asics
            .filter((asic) => asic?.id !== undefined)
            .map((asic) => getAsicId(hashboardSerialNumber, asic.id!)),
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
          // Check if ASIC exists using convenience hook (we'll need to call this inside the loop)
          // For now, use direct store access since we can't call hooks conditionally
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

      // Also update the telemetry store with inlet/outlet temperatures and average ASIC temp
      useMinerStore
        .getState()
        .telemetry.updateHashboardTemperatures(
          hashboardSerialNumber,
          data.inlet_temp_c
            ? { value: data.inlet_temp_c, units: "C" }
            : undefined,
          data.outlet_temp_c
            ? { value: data.outlet_temp_c, units: "C" }
            : undefined,
          data.avg_asic_temp_c
            ? { value: data.avg_asic_temp_c, units: "C" }
            : undefined,
          data.max_asic_temp_c
            ? { value: data.max_asic_temp_c, units: "C" }
            : undefined,
        );
    }
  }, [data, hashboardSerialNumber]);

  return useMemo(() => ({ pending, error, data }), [pending, error, data]);
};

export { useHashboardStatus };
