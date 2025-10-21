import { useCallback, useMemo, useState } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthErrors, useAuthHeader } from "@/protoOS/store";

interface RebootSystemProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
}

const useSystemReboot = () => {
  const { api } = useMinerHosting();
  const [pending, setPending] = useState<boolean>(false);
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();

  const rebootSystem = useCallback(
    ({ onError, onSuccess }: RebootSystemProps = {}) => {
      if (!api) return;

      setPending(true);
      api
        .rebootSystem(authHeader)
        .then(() => {
          onSuccess?.();
        })
        .catch((error) => {
          setPending(false);
          handleAuthErrors({
            error,
            onError,
            onSuccess: () => {
              rebootSystem({ onError, onSuccess });
            },
          });
        });
    },
    [authHeader, handleAuthErrors, api],
  );

  return useMemo(() => ({ pending, rebootSystem }), [pending, rebootSystem]);
};

export { useSystemReboot };
