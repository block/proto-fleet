import { useCallback, useMemo, useState } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store";

interface RebootSystemProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
}

const useSystemReboot = () => {
  const { adapter } = useMinerHosting();
  const [pending, setPending] = useState<boolean>(false);
  const authRetry = useAuthRetry();

  const rebootSystem = useCallback(
    ({ onError, onSuccess }: RebootSystemProps = {}) => {
      if (!adapter) return;

      setPending(true);
      authRetry({
        request: (header) => adapter.rebootSystem(header),
        onSuccess,
        onError: (error) => {
          setPending(false);
          onError?.(error);
        },
      });
    },
    [adapter, authRetry],
  );

  return useMemo(() => ({ pending, rebootSystem }), [pending, rebootSystem]);
};

export { useSystemReboot };
