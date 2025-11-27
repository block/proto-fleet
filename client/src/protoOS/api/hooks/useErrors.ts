import { useCallback, useEffect, useMemo, useState } from "react";

import { usePoll } from "./usePoll";
import { ErrorListResponse } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useSetErrors } from "@/protoOS/store";
import type { MinerError } from "@/protoOS/store/types";
import { transformErrors } from "@/protoOS/store/utils/errorTransformer";

type UseErrorsProps = {
  poll?: boolean;
  pollIntervalMs?: number;
};

const useErrors = ({ poll = false, pollIntervalMs }: UseErrorsProps = {}) => {
  const { api } = useMinerHosting();

  const [data, setData] = useState<ErrorListResponse>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const setErrors = useSetErrors();

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

  usePoll({
    fetchData,
    poll,
    pollIntervalMs,
  });

  // Transform and update store whenever errors change
  useEffect(() => {
    const transformedErrors: MinerError[] = transformErrors(data);
    setErrors(transformedErrors);
  }, [data, setErrors]);

  const response = useMemo(() => ({ fetchData, pending, error, data }), [fetchData, pending, error, data]);

  return response;
};

export { useErrors };
