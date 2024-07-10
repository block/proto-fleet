import { useCallback, useState } from "react";

import { api } from "./api";
import { TemperatureResponseTemperaturedata } from "./types";
import { usePoll } from "./usePoll";

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
  const [data, setData] = useState<TemperatureResponseTemperaturedata>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    if (!hashboardSerial) return;

    setPending(true);
    api
      .getHashboardTemperature(hashboardSerial, { duration })
      .then((res) => {
        setData(res?.data["temperature-data"]);
      })
      .catch((err) => {
        setError(err?.error?.message || err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [duration, hashboardSerial]);

  usePoll({
    data,
    fetchData,
    pending,
    poll,
  });

  return {
    pending,
    error,
    data,
  };
};

export { useHashboardTemperature };
