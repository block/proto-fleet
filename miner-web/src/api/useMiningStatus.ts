import { useCallback, useState } from "react";

import { api } from "./api";
import { MiningStatusMiningstatus } from "./types";
import { usePoll } from "./usePoll";

interface UseMiningStatusProps {
  poll?: boolean;
}

const useMiningStatus = ({ poll }: UseMiningStatusProps = {}) => {
  const [data, setData] = useState<MiningStatusMiningstatus>();
  const [error, setError] = useState();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    setPending(true);
    api
      .getMiningStatus()
      .then((res) => {
        setData(res?.data["mining-status"]);
      })
      .catch((err) => {
        setError(err?.error);
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

export { useMiningStatus };
