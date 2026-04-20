import { useCallback, useEffect, useMemo, useState } from "react";

import { ErrorListResponse } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useSetErrors } from "@/protoOS/store";
import { useAuthErrors } from "@/protoOS/store/hooks/useAuth";
import type { MinerError } from "@/protoOS/store/types";
import { transformErrors } from "@/protoOS/store/utils/errorTransformer";
import { usePoll } from "@/shared/hooks/usePoll";

type UseErrorsProps = {
  poll?: boolean;
  pollIntervalMs?: number;
};

const useErrors = ({ poll = false, pollIntervalMs }: UseErrorsProps = {}) => {
  const { api } = useMinerHosting();
  const { handleAuthErrors } = useAuthErrors();

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
        handleAuthErrors({
          error: err,
          onError: (e) => setError(e?.error?.message ?? "An error occurred"),
        });
      })
      .finally(() => {
        setPending(false);
      });
  }, [api, handleAuthErrors]);

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
