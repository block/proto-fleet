import { useCallback, useMemo, useState } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry, useCoolingMode, useFansTelemetry, useMinerStore } from "@/protoOS/store";
import type { CoolingMode } from "@/protoOS/store/contract";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
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
  const authRetry = useAuthRetry();
  const mode = useCoolingMode();
  const fans = useFansTelemetry();
  const data = useMemo(
    () =>
      loaded
        ? {
            mode,
            fans: fans.map((fan) => ({
              slot: fan.slot,
              rpm: fan.rpm?.latest?.value ?? undefined,
              percentage: fan.percentage?.latest?.value ?? undefined,
            })),
          }
        : undefined,
    [fans, loaded, mode],
  );

  const fetchData = useCallback(() => {
    if (!adapter) return;
    const accept = beginStoreWrite("cooling:module:root");
    setPending(true);
    return adapter
      .getCoolingStatus()
      .then((status) => {
        if (!accept()) return;
        const telemetry = useMinerStore.getState().telemetry;
        telemetry.updateCoolingMode(status.mode);
        status.fans.forEach((fan) => {
          const slot = Number(String(fan.componentId).match(/:(\d+)$/)?.[1]);
          if (slot)
            telemetry.updateFanTelemetry(slot, {
              slot,
              rpm: fan.rpm?.latest ? { latest: { value: fan.rpm.latest.value, units: "RPM" } } : undefined,
              percentage: fan.speedPercent?.latest
                ? { latest: { value: fan.speedPercent.latest.value, units: "%" } }
                : undefined,
            });
        });
      })
      .catch((err) => setError(err?.error?.message ?? "An error occurred"))
      .finally(() => {
        setLoaded(true);
        setPending(false);
      });
  }, [adapter]);

  usePoll({ fetchData, enabled, poll });

  const setCooling = useCallback(
    async ({ mode: nextMode, onSuccess, onError }: SetCoolingProps) => {
      if (!adapter) return;
      const accept = beginStoreWrite("cooling:module:root");
      setPending(true);
      await authRetry({
        request: (params) => adapter.setCoolingMode(nextMode, params),
        onSuccess: () => {
          if (accept()) useMinerStore.getState().telemetry.updateCoolingMode(nextMode);
          setPending(false);
          onSuccess?.();
        },
        onError: (requestError) => {
          onError?.(requestError);
          setPending(false);
        },
      });
    },
    [adapter, authRetry],
  );

  return useMemo(() => ({ pending, error, data, loaded, setCooling }), [pending, error, data, loaded, setCooling]);
};

export { useCoolingStatus };
