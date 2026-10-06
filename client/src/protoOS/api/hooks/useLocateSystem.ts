import { useCallback, useState } from "react";
import type { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import type { LocateSystemParams } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext/useMinerHosting";
import { useAuthRetry } from "@/protoOS/store";

interface UseLocateSystemParams {
  enable?: boolean;
  ledOnTime?: number;
  onError?: (error: ErrorProps) => void;
  onSuccess?: () => void;
}

export const useLocateSystem = () => {
  const { api } = useMinerHosting();
  const [pending, setPending] = useState(false);
  const authRetry = useAuthRetry();

  const locateSystem = useCallback(
    ({ enable, ledOnTime, onError, onSuccess }: UseLocateSystemParams) => {
      if (!api) return;

      const query: LocateSystemParams = {};
      if (enable !== undefined) query.enable = enable;
      if (ledOnTime !== undefined) query.led_on_time = ledOnTime;

      setPending(true);
      authRetry({
        request: (header) => api.locateSystem(query, header),
        onSuccess,
        onError,
      }).finally(() => setPending(false));
    },
    [api, authRetry],
  );

  return { pending, locateSystem };
};
