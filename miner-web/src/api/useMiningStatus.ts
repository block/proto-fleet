import { useEffect, useState } from "react";

import { api } from "./api";
import { MiningStatusMiningstatus } from "./types";

const useMiningStatus = () => {
  const [data, setData] = useState<MiningStatusMiningstatus>();
  const [error, setError] = useState();
  const [pending, setPending] = useState<boolean>(false);

  useEffect(() => {
    setPending(true);
    api.getMiningStatus()
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

  return {
    pending,
    error,
    data,
  };
};

export { useMiningStatus };
