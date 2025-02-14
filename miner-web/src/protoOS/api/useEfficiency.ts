import { useCallback, useState } from "react";

import { EfficiencyResponseEfficiencydata } from "./types";
import { usePoll } from "./usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface UseEfficiencyProps {
  duration: EfficiencyResponseEfficiencydata["duration"];
  poll?: boolean;
}

const useEfficiency = ({ duration, poll }: UseEfficiencyProps) => {
  const { api } = useMinerHosting();

  const [data, setData] = useState<EfficiencyResponseEfficiencydata>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    if (!api) return;

    setPending(true);
    api
      .getMinerEfficiency({ duration })
      .then((res) => {
        setData(res?.data["efficiency-data"]);
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

export { useEfficiency };
