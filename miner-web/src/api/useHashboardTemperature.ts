import { useCallback, useState } from "react";

import { api } from "./api";
import { Error, TemperatureResponseTemperaturedata } from "./types";
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
  const [error, setError] = useState<Error>();
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
        setError(err?.error || err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [duration, hashboardSerial]);

  usePoll({ fetchData, poll });

  return {
    pending,
    error,
    data,
  };
};

export { useHashboardTemperature };
