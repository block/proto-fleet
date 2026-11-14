import { useState } from "react";
import type { ControllerDiagnosticsResult } from "@/protoOS/api/adapters/controllerDiagnostics";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { usePoll } from "@/shared/hooks/usePoll";

type Adapter = ReturnType<typeof useMinerHosting>["adapter"];

interface Snapshot {
  adapter: Adapter;
  result?: ControllerDiagnosticsResult;
  error?: string;
}

// Role and reachability samples age out after 15 seconds, so refresh faster than that.
const POLL_INTERVAL_MS = 5_000;

/**
 * Controller diagnostics are read same-origin only. Fleet's miner proxy does
 * not forward the HA route, so an embedded ProtoOS hides them rather than
 * reporting a controller it never asked.
 */
export const useControllerDiagnosticsSupported = () => {
  const { adapter, isFleetHosted } = useMinerHosting();
  return Boolean(adapter?.getControllerDiagnostics) && !isFleetHosted;
};

export const useControllerDiagnostics = () => {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const [snapshot, setSnapshot] = useState<Snapshot>();
  const supported = useControllerDiagnosticsSupported();

  usePoll({
    params: adapter,
    enabled: supported,
    poll: true,
    pollIntervalMs: POLL_INTERVAL_MS,
    fetchData: async (signal) => {
      if (!adapter?.getControllerDiagnostics) return;
      await authRetry({
        signal,
        request: (options) => adapter.getControllerDiagnostics!(options),
        onSuccess: (result) => {
          if (!signal.aborted) setSnapshot({ adapter, result });
        },
        onError: () => {
          if (signal.aborted) return;
          // Keep the last result so its observation ages keep counting up.
          setSnapshot((previous) => ({
            adapter,
            result: previous?.adapter === adapter ? previous.result : undefined,
            error: "Could not reach this controller. Displayed controller details may be out of date.",
          }));
        },
      });
    },
  });

  const current = snapshot?.adapter === adapter ? snapshot : undefined;
  return { supported, result: current?.result, error: current?.error };
};
