import { useCallback, useState } from "react";

import { Granularity } from "pages/Hardware/types";

import { api } from "./api";
import { Error, TemperatureResponseTemperaturedata } from "./types";
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
  const [error, setError] = useState<Error>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    if (!hashboardSerial || asicID === undefined) return;

    setPending(true);
    api
      .getAsicTemperature(hashboardSerial, asicID, { duration, granularity })
      .then((res) => {
        setData(res?.data["temperature-data"]);
      })
      .catch((err) => {
        setError(err?.error || err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [duration, granularity, hashboardSerial, asicID]);

  usePoll({ fetchData, poll });

  return {
    pending,
    error,
    data,
  };
};

export { useAsicTemperature };
