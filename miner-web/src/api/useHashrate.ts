import { useCallback, useState } from "react";

import { api } from "./api";
import { HashrateResponseHashratedata } from "./types";
import { usePoll } from "./usePoll";

interface UseHashrateProps {
  duration: HashrateResponseHashratedata["duration"];
  poll?: boolean;
}

const useHashrate = ({ duration, poll }: UseHashrateProps) => {
  const [data, setData] = useState<HashrateResponseHashratedata>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    setPending(true);
    api
      .getMinerHashrate({ duration })
      .then((res) => {
        setData(res?.data["hashrate-data"]);
      })
      .catch((err) => {
        setError(err?.error?.message || err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [duration]);

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

export { useHashrate };
