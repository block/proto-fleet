import { useCallback, useMemo, useState } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthErrors, useAuthHeader } from "@/protoOS/store";

interface StartMiningProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
}

const useMiningStart = () => {
  const { api } = useMinerHosting();
  const [pending, setPending] = useState<boolean>(false);
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();

  const startMining = useCallback(
    ({ onError, onSuccess }: StartMiningProps = {}) => {
      if (!api) return;

      const performStart = () => {
        setPending(true);
        api
          .startMining(authHeader)
          .then(() => {
            onSuccess?.();
          })
          .catch((error) => {
            handleAuthErrors({
              error,
              onError,
              onSuccess: () => {
                performStart();
              },
            });
          })
          .finally(() => {
            setPending(false);
          });
      };

      performStart();
    },
    [authHeader, handleAuthErrors, api],
  );

  return useMemo(() => ({ pending, startMining }), [pending, startMining]);
};

export { useMiningStart };
