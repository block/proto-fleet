import { useCallback, useEffect, useMemo, useState } from "react";

import { HashboardStatsHashboardstats } from "./types";
import { usePoll } from "./usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import useHashboardAsicStore from "@/protoOS/store/useHashboardAsicStore";
interface UseHashboardStatsProps {
  hashboardSerialNumber: string;
  poll?: boolean;
}

const useHashboardStats = ({
  hashboardSerialNumber,
  poll,
}: UseHashboardStatsProps) => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<HashboardStatsHashboardstats>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const {
    updateCompleteAsicData,
    initializeHashboardAsics,
    updateBoardHashrate,
    updateAvgAsicTemp,
    updatePowerUsage,
  } = useHashboardAsicStore();

  const fetchData = useCallback(() => {
    if (!api) return;

    setPending(true);
    api
      .getHashboardStatus(hashboardSerialNumber)
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
        console.warn(
          `No ASIC data found for hashboard ${hashboardSerialNumber}`,
        );
        return;
      }
      const asicIds = asics
        .map((asic) => asic.id)
        .filter((id): id is number => id !== undefined);
      initializeHashboardAsics(hashboardSerialNumber, asicIds);

      for (const asic of asics) {
        if (asic !== undefined) {
          updateCompleteAsicData(hashboardSerialNumber, asic?.id ?? 0, asic);
        }
      }

      updateAvgAsicTemp(hashboardSerialNumber, data.avg_asic_temp_c ?? 0);
      updateBoardHashrate(hashboardSerialNumber, data.hashrate_ghs ?? 0);
      updatePowerUsage(hashboardSerialNumber, data.power_usage_watts ?? 0);
    }
  }, [data]);

  return useMemo(() => ({ pending, error, data }), [pending, error, data]);
};

export { useHashboardStats };
