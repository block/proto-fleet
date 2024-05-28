import { useCallback, useState } from "react";

import { api } from "./api";
import { HashboardsInfoHashboardsinfo } from "./types";
import { usePoll } from "./usePoll";

interface UseHashboardsProps {
  poll?: boolean;
}

const useHashboards = ({ poll }: UseHashboardsProps = {}) => {
  const [data, setData] = useState<HashboardsInfoHashboardsinfo[]>();
  const [error, setError] = useState();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    setPending(true);
    api.getAllHashboards()
      .then((res) => {
        setData(res?.data["hashboards-info"]);
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

export { useHashboards };
