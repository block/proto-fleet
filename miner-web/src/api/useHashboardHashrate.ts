import { useCallback, useState } from "react";

import { api } from "./api";
import { Error, HashrateResponseHashratedata } from "./types";
import { usePoll } from "./usePoll";

interface UseHashboardHashrateProps {
  duration: HashrateResponseHashratedata["duration"];
  hashboardSerial: string;
  poll?: boolean;
}

const useHashboardHashrate = ({
  duration,
  hashboardSerial,
  poll,
}: UseHashboardHashrateProps) => {
  const [data, setData] = useState<HashrateResponseHashratedata>();
  const [error, setError] = useState<Error>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    if (!hashboardSerial) return;
    setPending(true);
    api
      .getHashboardHashrate(hashboardSerial, { duration })
      .then((res) => {
        setData(res?.data["hashrate-data"]);
      })
      .catch((err) => {
        setError(err?.error || { message: err });
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

export { useHashboardHashrate };
