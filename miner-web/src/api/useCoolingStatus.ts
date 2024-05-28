import { useState } from "react";

import { api } from "./api";
import { CoolingStatusCoolingstatus } from "./types";
import { usePoll } from "./usePoll";

interface UseCoolingStatusProps {
  poll?: boolean;
}

const useCoolingStatus = ({ poll }: UseCoolingStatusProps = {}) => {
  const [data, setData] = useState<CoolingStatusCoolingstatus>();
  const [error, setError] = useState();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = () => {
    setPending(true);
    api
      .getCooling()
      .then((res) => {
        setData(res?.data["cooling-status"]);
      })
      .catch((err) => {
        setError(err?.error);
      })
      .finally(() => {
        setPending(false);
      });
  };

  usePoll({ fetchData, poll });

  return {
    pending,
    error,
    data,
  };
};

export { useCoolingStatus };
