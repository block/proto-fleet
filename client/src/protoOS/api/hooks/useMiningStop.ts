import { useCallback, useMemo, useState } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthErrors, useAuthHeader } from "@/protoOS/store";

interface StopMiningProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
}

const useMiningStop = () => {
  const { api } = useMinerHosting();
  const [pending, setPending] = useState<boolean>(false);
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();

  const stopMining = useCallback(
    ({ onError, onSuccess }: StopMiningProps = {}) => {
      if (!api) return;

      setPending(true);
      api
        .stopMining(authHeader)
        .then(() => {
          onSuccess?.();
        })
        .catch((error) => {
          handleAuthErrors({
            error,
            onError,
            onSuccess: () => {
              stopMining({ onError, onSuccess });
            },
          });
        })
        .finally(() => {
          setPending(false);
        });
    },
    [authHeader, handleAuthErrors, api],
  );

  return useMemo(() => ({ pending, stopMining }), [pending, stopMining]);
};

export { useMiningStop };
