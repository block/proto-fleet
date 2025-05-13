import { useCallback, useMemo } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

interface HandleAuthErrorsProps {
  error: ErrorProps;
  onError?: (err: ErrorProps) => void;
  onSuccess?: (accessToken: string) => void;
}

const useAuthErrors = () => {
  const handleAuthErrors = useCallback(
    ({ error, onError }: HandleAuthErrorsProps) => {
      onError?.(error);
    },
    [],
  );

  return useMemo(
    () => ({
      handleAuthErrors,
    }),
    [handleAuthErrors],
  );
};

export { useAuthErrors };
