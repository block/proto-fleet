import { useCallback, useState } from "react";

import { api } from "api";
import { MiningStatusMiningstatus } from "apiTypes";

interface getMiningStatusProps {
  onSuccess?: (res?: MiningStatusMiningstatus) => void;
}

const useMiningStatus = () => {
  const [data, setData] = useState<MiningStatusMiningstatus>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(
    ({ onSuccess }: getMiningStatusProps = {}) => {
      setPending(true);
      api
        .getMiningStatus()
        .then((res) => {
          setData(res?.data["mining-status"]);
          onSuccess?.(res?.data["mining-status"]);
        })
        .catch((err) => {
          setError(err?.error?.message ?? err);
        })
        .finally(() => {
          setPending(false);
        });
    },
    []
  );

  return {
    fetchData,
    data,
    pending,
    error,
  };
};

export { useMiningStatus };
