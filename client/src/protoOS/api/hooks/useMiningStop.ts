import { useCallback, useMemo, useState } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store";

interface StopMiningProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
}

const useMiningStop = () => {
  const { adapter } = useMinerHosting();
  const [pending, setPending] = useState<boolean>(false);
  const authRetry = useAuthRetry();

  const stopMining = useCallback(
    ({ onError, onSuccess }: StopMiningProps = {}) => {
      if (!adapter) return;

      setPending(true);
      authRetry({
        request: (header) => adapter.stopMining(header),
        onSuccess,
        onError,
      }).finally(() => setPending(false));
    },
    [adapter, authRetry],
  );

  return useMemo(() => ({ pending, stopMining }), [pending, stopMining]);
};

export { useMiningStop };
