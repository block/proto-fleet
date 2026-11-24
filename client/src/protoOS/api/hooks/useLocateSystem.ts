import { useCallback, useState } from "react";
import type { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext/useMinerHosting";
import { useAuthRetry } from "@/protoOS/store";

interface UseLocateSystemParams {
  enable?: boolean;
  ledOnTime?: number;
  onError?: (error: ErrorProps) => void;
  onSuccess?: () => void;
}

export const useLocateSystem = () => {
  const { adapter } = useMinerHosting();
  const [pending, setPending] = useState(false);
  const authRetry = useAuthRetry();

  const locateSystem = useCallback(
    ({ enable, ledOnTime, onError, onSuccess }: UseLocateSystemParams) => {
      if (!adapter) return;

      setPending(true);
      authRetry({
        request: (header) => adapter.locateSystem({ enable, ledOnTime }, header),
        onSuccess,
        onError,
      }).finally(() => setPending(false));
    },
    [adapter, authRetry],
  );

  return { pending, locateSystem };
};
