import { useCallback, useMemo, useRef, useState } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry, useCoolingMode, useFansTelemetry, useMinerStore } from "@/protoOS/store";
import type { CoolingMode } from "@/protoOS/store/contract";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import { getComponentSlot } from "@/protoOS/store/utils/entityIds";
import { usePoll } from "@/shared/hooks/usePoll";

interface UseCoolingStatusProps {
  poll?: boolean;
  enabled?: boolean;
}

interface SetCoolingProps {
  mode: CoolingMode;
  onSuccess?: () => void;
  onError?: (error: ErrorProps) => void;
}

const useCoolingStatus = ({ poll, enabled = true }: UseCoolingStatusProps = {}) => {
  const { adapter } = useMinerHosting();
  const [error, setError] = useState<string>();
  const [loaded, setLoaded] = useState(false);
  const [pending, setPending] = useState(false);
  const nextCommandIdRef = useRef(0);
  const activeCommandIdRef = useRef<number | undefined>(undefined);
  const authRetry = useAuthRetry();
  const mode = useCoolingMode();
  const fans = useFansTelemetry();
  const data = useMemo(
    () =>
      loaded
        ? {
            mode,
            fans: fans.map((fan) => ({
              slot: getComponentSlot(fan.componentId),
              rpm: fan.rpm?.latest?.value ?? undefined,
              percentage: fan.speedPercent?.latest?.value ?? undefined,
            })),
          }
        : undefined,
    [fans, loaded, mode],
  );

  const fetchData = useCallback(() => {
    if (!adapter || activeCommandIdRef.current !== undefined) return;
    const commandIdAtStart = nextCommandIdRef.current;
    const accept = beginStoreWrite("cooling:read:module:root");
    setPending(true);
    return adapter
      .getCoolingStatus()
      .then((status) => {
        if (commandIdAtStart !== nextCommandIdRef.current || activeCommandIdRef.current !== undefined || !accept())
          return;
        const telemetry = useMinerStore.getState().telemetry;
        telemetry.updateCoolingMode(status.mode);
        telemetry.replaceFanLiveTelemetry(status.fans);
      })
      .catch((err) => setError(err?.error?.message ?? "An error occurred"))
      .finally(() => {
        setLoaded(true);
        if (activeCommandIdRef.current === undefined) setPending(false);
      });
  }, [adapter]);

  usePoll({ fetchData, enabled, poll });

  const setCooling = useCallback(
    async ({ mode: nextMode, onSuccess, onError }: SetCoolingProps) => {
      if (!adapter) return;
      const commandId = nextCommandIdRef.current + 1;
      nextCommandIdRef.current = commandId;
      activeCommandIdRef.current = commandId;
      const accept = beginStoreWrite("cooling:command:module:root");
      setPending(true);
      await authRetry({
        request: (params) => adapter.setCoolingMode(nextMode, params),
        onSuccess: () => {
          const isCurrentCommand = activeCommandIdRef.current === commandId;
          if (isCurrentCommand) activeCommandIdRef.current = undefined;
          const accepted = isCurrentCommand && accept();
          if (isCurrentCommand) setPending(false);
          if (accepted) {
            useMinerStore.getState().telemetry.updateCoolingMode(nextMode);
            void fetchData();
          }
          // A superseded command must not fire the caller's callback: it would
          // surface a stale toast/side effect for a request the user replaced.
          if (isCurrentCommand) onSuccess?.();
        },
        onError: (requestError) => {
          const isCurrentCommand = activeCommandIdRef.current === commandId;
          if (isCurrentCommand) activeCommandIdRef.current = undefined;
          if (isCurrentCommand) {
            onError?.(requestError);
            setPending(false);
          }
        },
      });
    },
    [adapter, authRetry, fetchData],
  );

  return useMemo(() => ({ pending, error, data, loaded, setCooling }), [pending, error, data, loaded, setCooling]);
};

export { useCoolingStatus };
