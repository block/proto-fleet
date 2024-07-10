import { useCallback, useState } from "react";

import { api } from "./api";
import { EfficiencyResponseEfficiencydata } from "./types";
import { usePoll } from "./usePoll";

interface UseEfficiencyProps {
  duration: EfficiencyResponseEfficiencydata["duration"];
  poll?: boolean;
}

const useEfficiency = ({ duration, poll }: UseEfficiencyProps) => {
  const [data, setData] = useState<EfficiencyResponseEfficiencydata>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    setPending(true);
    api
      .getMinerEfficiency({ duration })
      .then((res) => {
        setData(res?.data["efficiency-data"]);
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

export { useEfficiency };
