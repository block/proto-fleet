import { useCallback, useEffect, useState } from "react";

import { Granularity } from "pages/Hardware/types";

import { api } from "./api";
import { HashrateResponseHashratedata } from "./types";
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
      .getAsicHashrate(hashboardSerial, asicID, { duration, granularity })
      .then((res) => {
        setData(res?.data["hashrate-data"]);
      })
      .catch((err) => {
        setError(err?.error?.message || err);
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

export { useAsicHashrate };
