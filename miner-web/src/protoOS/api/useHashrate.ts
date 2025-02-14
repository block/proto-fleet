import { useCallback, useState } from "react";

import { HashrateResponseHashratedata } from "./types";
import { usePoll } from "./usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface UseHashrateProps {
  duration: HashrateResponseHashratedata["duration"];
  poll?: boolean;
}

const useHashrate = ({ duration, poll }: UseHashrateProps) => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<HashrateResponseHashratedata>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    if (!api) return;

    setPending(true);
    api
      .getMinerHashrate({ duration })
      .then((res) => {
        setData(res?.data["hashrate-data"]);
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [duration, api]);

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

export { useHashrate };
