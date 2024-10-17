import { useCallback, useState } from "react";

import { api } from "./api";
import { ErrorListResponse } from "./types";

const useErrors = () => {
  const [data, setData] = useState<ErrorListResponse>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    setPending(true);
    api
      .getErrors()
      .then((res) => {
        setData(res?.data);
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, []);

  return {
    fetchData,
    pending,
    error,
    data,
  };
};

export { useErrors };
