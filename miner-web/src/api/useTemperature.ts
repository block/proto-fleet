import { useCallback, useState } from "react";

import { api } from "./api";
import { Error, TemperatureResponseTemperaturedata } from "./types";
import { usePoll } from "./usePoll";

interface UseTemperatureProps {
  duration: TemperatureResponseTemperaturedata["duration"];
  poll?: boolean;
}

const useTemperature = ({ duration, poll }: UseTemperatureProps) => {
  const [data, setData] = useState<TemperatureResponseTemperaturedata>();
  const [error, setError] = useState<Error>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    setPending(true);
    api
      .getMinerTemperature({ duration })
      .then((res) => {
        setData(res?.data["temperature-data"]);
      })
      .catch((err) => {
        setError(err?.error || { message: err });
      })
      .finally(() => {
        setPending(false);
      });
  }, [duration]);

  usePoll({ fetchData, poll });

  return {
    pending,
    error,
    data,
  };
};

export { useTemperature };
