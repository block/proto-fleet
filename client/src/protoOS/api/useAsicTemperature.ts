import { useCallback, useEffect, useState } from "react";

import {
  GetAsicTemperatureParams,
  TemperatureResponseTemperaturedata,
} from "./types";
import { usePoll } from "./usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { type Duration } from "@/shared/components/DurationSelector";
import useHashboardAsicStore, {
  HistoricalData,
} from "@/protoOS/store/useHashboardAsicStore";

interface UseAsicTemperatureProps {
  asicId?: number;
  duration: Duration;
  granularity: GetAsicTemperatureParams["granularity"];
  hashboardSerial?: string;
  poll?: boolean;
}

const useAsicTemperature = ({
  asicId,
  duration,
  granularity,
  hashboardSerial,
  poll,
}: UseAsicTemperatureProps) => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<TemperatureResponseTemperaturedata>();
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
      .getAsicTemperature({
        hbSn: hashboardSerial,
        asicId,
        duration,
        granularity,
      })
      .then((res) => {
        setData(res?.data["temperature-data"]);
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [duration, granularity, hashboardSerial, asicId, api]);

  useEffect(() => {
    if (!data || !hashboardSerial || asicId === undefined) return;

    initializeAsic(hashboardSerial, asicId);
    const updateData: HistoricalData = {};

    if (data?.data) {
      updateData.tempHistory = data.data;
    }

    if (data?.aggregates) {
      updateData.tempAggregates = data.aggregates;
    }

    updateAsicHistoricalData(hashboardSerial, asicId, updateData);
    updateAsicCurrentData(hashboardSerial, asicId, {
      temp: data.data?.[data.data.length - 1].value,
    });
  }, [data, hashboardSerial]);

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

  usePoll({
    fetchData,
    params,
    poll,
  });

  return {
    pending,
    error,
    data,
  };
};

export { useAsicTemperature };
