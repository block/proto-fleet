import { useCallback, useState } from "react";

import { api } from "./api";
import { Error, TemperatureResponseTemperaturedata } from "./types";
import { usePoll } from "./usePoll";

interface UseAsicTemperatureProps {
  asicID?: number;
  duration: TemperatureResponseTemperaturedata["duration"];
  hashboardSerial?: string;
  poll?: boolean;
}

const useAsicTemperature = ({
  asicID,
  duration,
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
      .getAsicTemperature(hashboardSerial, asicID, { duration })
      .then((res) => {
        setData(res?.data["temperature-data"]);
      })
      .catch((err) => {
        setError(err?.error || { message: err });
      })
      .finally(() => {
        setPending(false);
      });
  }, [duration, hashboardSerial, asicID]);

  usePoll({ fetchData, poll });

  return {
    pending,
    error,
    data,
  };
};

export { useAsicTemperature };
