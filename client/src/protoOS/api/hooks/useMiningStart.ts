import { useCallback, useMemo, useState } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store";

interface StartMiningProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
}

const useMiningStart = () => {
  const { api } = useMinerHosting();
  const [pending, setPending] = useState<boolean>(false);
  const authRetry = useAuthRetry();

  const startMining = useCallback(
    ({ onError, onSuccess }: StartMiningProps = {}) => {
      if (!api) return;

      setPending(true);
      authRetry({
        request: (header) => api.startMining(header),
        onSuccess,
        onError,
      }).finally(() => setPending(false));
    },
    [api, authRetry],
  );

  return useMemo(() => ({ pending, startMining }), [pending, startMining]);
};

export { useMiningStart };
