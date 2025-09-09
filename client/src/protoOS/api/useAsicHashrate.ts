import { useCallback, useEffect, useMemo, useState } from "react";

import { GetAsicHashrateParams, HashrateResponseHashratedata } from "./types";
import { usePoll } from "./usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import useHashboardAsicStore, {
  HistoricalData,
} from "@/protoOS/store/useHashboardAsicStore";
import { type Duration } from "@/shared/components/DurationSelector";

interface UseAsicHashrateProps {
  asicId?: number;
  duration: Duration;
  granularity: GetAsicHashrateParams["granularity"];
  hashboardSerial?: string;
  poll?: boolean;
}

const useAsicHashrate = ({
  asicId,
  duration,
  granularity,
  hashboardSerial,
  poll,
}: UseAsicHashrateProps) => {
  const { api } = useMinerHosting();

  const [data, setData] = useState<HashrateResponseHashratedata>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const [params, setParams] = useState({
    asicId,
    duration,
    granularity,
    hashboardSerial,
  });
  const { updateAsicHistoricalData, updateAsicCurrentData, initializeAsic } =
    useHashboardAsicStore();

  const fetchData = useCallback(() => {
    if (!hashboardSerial || asicId === undefined || !api) return;

    setPending(true);
    api
      .getAsicHashrate({ hbSn: hashboardSerial, asicId, duration, granularity })
      .then((res) => {
        setData(res?.data["hashrate-data"]);
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [duration, granularity, hashboardSerial, asicId, api]);

  useEffect(() => {
    if (
      asicId !== params.asicId ||
      duration !== params.duration ||
      granularity !== params.granularity ||
      hashboardSerial !== params.hashboardSerial
    ) {
      setParams({ asicId, duration, granularity, hashboardSerial });
    }
  }, [asicId, duration, granularity, hashboardSerial, params]);

  useEffect(() => {
    if (!data || !hashboardSerial || asicId === undefined) return;

    initializeAsic(hashboardSerial, asicId);
    const updateData: HistoricalData = {};

    if (data?.data) {
      updateData.hashrateHistory = data.data;
    }

    if (data?.aggregates) {
      updateData.hashrateAggregates = data.aggregates;
    }

    updateAsicHistoricalData(hashboardSerial, asicId, updateData);
    const current = data.data?.[data.data.length - 1].value;
    current &&
      updateAsicCurrentData(hashboardSerial, asicId, {
        hashrate: current / 1e3,
      });
  }, [
    data,
    asicId,
    hashboardSerial,
    initializeAsic,
    updateAsicCurrentData,
    updateAsicHistoricalData,
  ]);

  usePoll({
    fetchData,
    params,
    poll,
  });

  const response = useMemo(
    () => ({ pending, error, data }),
    [pending, error, data],
  );

  return response;
};

export { useAsicHashrate };
