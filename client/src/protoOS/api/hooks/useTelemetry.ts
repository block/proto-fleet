import { useCallback, useMemo, useState } from "react";

import type { TelemetryLevel } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useMinerStore } from "@/protoOS/store";
import { useAuthErrors } from "@/protoOS/store/hooks/useAuth";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import { usePoll } from "@/shared/hooks/usePoll";

interface UseTelemetryProps {
  level?: TelemetryLevel[];
  poll?: boolean;
  pollIntervalMs?: number;
  enabled?: boolean;
}

const useTelemetry = ({
  level = ["miner", "hashboard"],
  poll = true,
  pollIntervalMs = 15_000,
  enabled = true,
}: UseTelemetryProps = {}) => {
  const { adapter } = useMinerHosting();
  const { handleAuthErrors } = useAuthErrors();
  const data = useMinerStore((state) => state.telemetry);
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState(false);

  const fetchData = useCallback(async () => {
    if (!adapter) return;
    setPending(true);
    const levelScope = [...new Set(level)].sort().join(",") || "none";
    const accept = beginStoreWrite(`telemetry:current:${levelScope}`);
    setError(undefined);
    try {
      const normalized = await adapter.getCurrentTelemetry(level);
      if (accept()) useMinerStore.getState().telemetry.mergeTelemetry(normalized, accept.requestId);
    } catch (err) {
      handleAuthErrors({
        error: err as never,
        onError: (response) =>
          setError(response?.error?.message ?? (err instanceof Error ? err.message : "Unknown error occurred")),
      });
    } finally {
      setPending(false);
    }
  }, [adapter, handleAuthErrors, level]);

  usePoll({ fetchData, enabled, poll, pollIntervalMs });
  return useMemo(() => ({ pending, error, data }), [pending, error, data]);
};

export { useTelemetry };
