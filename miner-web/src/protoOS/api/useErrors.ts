import { useCallback, useMemo, useState } from "react";

import { ErrorListResponse } from "./types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

const useErrors = () => {
  const { api } = useMinerHosting();

  const [data, setData] = useState<ErrorListResponse>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    if (!api) return;

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
  }, [api]);

  const response = useMemo(
    () => ({ fetchData, pending, error, data }),
    [fetchData, pending, error, data],
  );

  return response;
};

export { useErrors };
