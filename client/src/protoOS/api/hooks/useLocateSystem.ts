import { useCallback, useState } from "react";
import type { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext/useMinerHosting";
import { useAuthRetry } from "@/protoOS/store";

interface UseLocateSystemParams {
  ledOnTime?: number;
  onError?: (error: ErrorProps) => void;
  onSuccess?: () => void;
}

export const useLocateSystem = () => {
  const { api } = useMinerHosting();
  const [pending, setPending] = useState(false);
  const authRetry = useAuthRetry();

  const locateSystem = useCallback(
    ({ ledOnTime = 30, onError, onSuccess }: UseLocateSystemParams) => {
      if (!api) return;

      setPending(true);
      authRetry({
        request: (header) => api.locateSystem({ led_on_time: ledOnTime }, header),
        onSuccess,
        onError,
      }).finally(() => setPending(false));
    },
    [api, authRetry],
  );

  return { pending, locateSystem };
};
