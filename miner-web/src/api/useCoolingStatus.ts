import { useEffect, useState } from "react";

import { api } from "./api";
import { CoolingStatusCoolingstatus } from "./types";

const useCoolingStatus = () => {
  const [data, setData] = useState<CoolingStatusCoolingstatus>();
  const [error, setError] = useState();
  const [pending, setPending] = useState<boolean>(false);

  useEffect(() => {
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
  }, []);

  return {
    pending,
    error,
    data,
  };
};

export { useCoolingStatus };
