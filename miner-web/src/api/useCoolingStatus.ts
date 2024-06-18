import { useCallback, useState } from "react";

import { api } from "./api";
import { CoolingStatusCoolingstatus, Error } from "./types";
import { usePoll } from "./usePoll";

interface UseCoolingStatusProps {
  poll?: boolean;
}

const useCoolingStatus = ({ poll }: UseCoolingStatusProps = {}) => {
  const [data, setData] = useState<CoolingStatusCoolingstatus>();
  const [error, setError] = useState<Error>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    setPending(true);
    api
      .getCooling()
      .then((res) => {
        setData(res?.data["cooling-status"]);
      })
      .catch((err) => {
        setError(err?.error || err);
      })
      .finally(() => {
        setPending(false);
      });
  }, []);

  usePoll({ fetchData, poll });

  return {
    pending,
    error,
    data,
  };
};

export { useCoolingStatus };
