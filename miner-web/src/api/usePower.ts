import { useCallback, useState } from "react";

import { api } from "./api";
import { PowerResponsePowerdata } from "./types";
import { usePoll } from "./usePoll";

interface UsePowerProps {
  duration: PowerResponsePowerdata["duration"];
  poll?: boolean;
}

const usePower = ({ duration, poll }: UsePowerProps) => {
  const [data, setData] = useState<PowerResponsePowerdata>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    setPending(true);
    api
      .getMinerPower({ duration })
      .then((res) => {
        setData(res?.data["power-data"]);
      })
      .catch((err) => {
        setError(err?.error?.message || err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [duration]);

  usePoll({
    fetchData,
    params: duration,
    poll,
  });

  return {
    pending,
    error,
    data,
  };
};

export { usePower };
