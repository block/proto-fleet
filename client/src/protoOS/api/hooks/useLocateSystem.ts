import { useCallback, useState } from "react";
import type { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext/useMinerHosting";
import { useAuthErrors, useAuthHeader } from "@/protoOS/store";

interface UseLocateSystemParams {
  ledOnTime?: number;
  onError?: (error: ErrorProps) => void;
  onSuccess?: () => void;
}

export const useLocateSystem = () => {
  const { api } = useMinerHosting();
  const [pending, setPending] = useState(false);
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();

  const locateSystem = useCallback(
    ({ ledOnTime = 30, onError, onSuccess }: UseLocateSystemParams) => {
      if (!api) return;

      const performLocate = () => {
        setPending(true);
        api
          .locateSystem({ led_on_time: ledOnTime }, authHeader)
          .then(() => {
            if (onSuccess) onSuccess();
          })
          .catch((error) => {
            handleAuthErrors({ error, onError, onSuccess: performLocate });
          })
          .finally(() => setPending(false));
      };

      performLocate();
    },
    [api, authHeader, handleAuthErrors],
  );

  return { pending, locateSystem };
};
