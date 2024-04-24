import { useEffect, useState } from "react";

import { api } from "./api";
import { HashboardsInfoHashboardsinfo } from "./types";

const useHashboards = () => {
  const [data, setData] = useState<HashboardsInfoHashboardsinfo[]>();
  const [error, setError] = useState();
  const [pending, setPending] = useState<boolean>(false);

  useEffect(() => {
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

  return {
    pending,
    error,
    data,
  };
};

export { useHashboards };
