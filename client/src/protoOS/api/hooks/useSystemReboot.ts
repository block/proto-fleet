import { useCallback, useMemo, useState } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store";

interface RebootSystemProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
}

const useSystemReboot = () => {
  const { api } = useMinerHosting();
  const [pending, setPending] = useState<boolean>(false);
  const authRetry = useAuthRetry();

  const rebootSystem = useCallback(
    ({ onError, onSuccess }: RebootSystemProps = {}) => {
      if (!api) return;

      setPending(true);
      authRetry({
        request: (header) => api.rebootSystem(header),
        onSuccess,
        onError: (error) => {
          setPending(false);
          onError?.(error);
        },
      });
    },
    [api, authRetry],
  );

  return useMemo(() => ({ pending, rebootSystem }), [pending, rebootSystem]);
};

export { useSystemReboot };
