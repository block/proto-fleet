import { useCallback, useState } from "react";

import { Granularity } from "pages/Hardware/types";

import { api } from "./api";
import { Error, HashrateResponseHashratedata } from "./types";
import { usePoll } from "./usePoll";

interface UseAsicHashrateProps {
  asicID?: number;
  duration: HashrateResponseHashratedata["duration"];
  granularity: Granularity;
  hashboardSerial?: string;
  poll?: boolean;
}

const useAsicHashrate = ({
  asicID,
  duration,
  granularity,
  hashboardSerial,
  poll,
}: UseAsicHashrateProps) => {
  const [data, setData] = useState<HashrateResponseHashratedata>();
  const [error, setError] = useState<Error>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    if (!hashboardSerial || asicID === undefined) return;

    setPending(true);
    api
      .getAsicHashrate(hashboardSerial, asicID, { duration, granularity })
      .then((res) => {
        setData(res?.data["hashrate-data"]);
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

export { useAsicHashrate };
