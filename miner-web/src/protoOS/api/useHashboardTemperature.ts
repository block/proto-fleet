import { useCallback, useEffect, useMemo, useState } from "react";

import { TemperatureResponseTemperaturedata } from "./types";
import { usePoll } from "./usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface UseHashboardTemperatureProps {
  duration: TemperatureResponseTemperaturedata["duration"];
  hashboardSerial?: string;
  poll?: boolean;
}

const useHashboardTemperature = ({
  duration,
  hashboardSerial,
  poll,
}: UseHashboardTemperatureProps) => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<TemperatureResponseTemperaturedata>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const [params, setParams] = useState({ duration, hashboardSerial });

  const fetchData = useCallback(() => {
    if (!hashboardSerial || !api) return;

    setPending(true);
    api
      .getHashboardTemperature({ hbSn: hashboardSerial, duration })
      .then((res) => {
        setData(res?.data["temperature-data"]);
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [duration, hashboardSerial, api]);

  useEffect(() => {
    if (
      duration !== params.duration ||
      hashboardSerial !== params.hashboardSerial
    ) {
      setParams({ duration, hashboardSerial });
    }
  }, [duration, hashboardSerial, params]);

  usePoll({
    fetchData,
    params,
    poll,
  });

  return useMemo(() => ({ pending, error, data }), [pending, error, data]);
};

export { useHashboardTemperature };
