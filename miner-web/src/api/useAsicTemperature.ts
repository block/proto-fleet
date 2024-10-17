import { useCallback, useEffect, useState } from "react";

import { Granularity } from "pages/Temperature/types";

import { api } from "./api";
import { TemperatureResponseTemperaturedata } from "./types";
import { usePoll } from "./usePoll";

interface UseAsicTemperatureProps {
  asicID?: number;
  duration: TemperatureResponseTemperaturedata["duration"];
  granularity: Granularity;
  hashboardSerial?: string;
  poll?: boolean;
}

const useAsicTemperature = ({
  asicID,
  duration,
  granularity,
  hashboardSerial,
  poll,
}: UseAsicTemperatureProps) => {
  const [data, setData] = useState<TemperatureResponseTemperaturedata>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const [params, setParams] = useState({
    asicID,
    duration,
    granularity,
    hashboardSerial,
  });

  const fetchData = useCallback(() => {
    if (!hashboardSerial || asicID === undefined) return;

    setPending(true);
    api
      .getAsicTemperature(hashboardSerial, asicID, { duration, granularity })
      .then((res) => {
        setData(res?.data["temperature-data"]);
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [duration, granularity, hashboardSerial, asicID]);

  useEffect(() => {
    if (
      asicID !== params.asicID ||
      duration !== params.duration ||
      granularity !== params.granularity ||
      hashboardSerial !== params.hashboardSerial
    ) {
      setParams({ asicID, duration, granularity, hashboardSerial });
    }
  }, [asicID, duration, granularity, hashboardSerial, params]);

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
