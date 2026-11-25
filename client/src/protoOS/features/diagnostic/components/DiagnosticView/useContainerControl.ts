import { useRef, useState } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import useMinerStore from "@/protoOS/store/useMinerStore";

export const useContainerControl = () => {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const inFlight = useRef(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();

  const setControl = async (name: string, enabled: boolean) => {
    if (!adapter?.setContainerControl || inFlight.current) return;
    inFlight.current = true;
    setPending(true);
    setError(undefined);
    const generation = useMinerStore.getState().connectionGeneration;
    const isCurrent = () => useMinerStore.getState().connectionGeneration === generation;
    let commanded = false;
    try {
      await authRetry({
        shouldRetry: () => !commanded && isCurrent(),
        request: async (params) => {
          if (!isCurrent()) return;
          await adapter.setContainerControl!(name, enabled, params);
          commanded = true;
        },
        onSuccess: async () => {
          if (!isCurrent()) return;
          const accept = beginStoreWrite("telemetry:current:miner");
          const telemetry = await adapter.getCurrentTelemetry(["miner"]);
          if (accept()) useMinerStore.getState().telemetry.mergeTelemetry(telemetry, accept.requestId);
        },
        onError: () => {
          if (!isCurrent()) return;
          setError(
            commanded
              ? "Command sent. Could not refresh control status; waiting for the next update."
              : "Could not change the control. Try again.",
          );
        },
      });
    } finally {
      inFlight.current = false;
      setPending(false);
    }
  };

  return { pending, error, setControl, supported: Boolean(adapter?.setContainerControl) };
};
