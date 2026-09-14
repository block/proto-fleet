import { useRef, useState } from "react";

import type { MiningScope } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import useMinerStore from "@/protoOS/store/useMinerStore";

export const useTankPower = () => {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const inFlight = useRef(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();

  const setPower = async (scope: MiningScope | undefined, on: boolean) => {
    if (!adapter || !scope?.modules?.length || inFlight.current) return;
    const command = on ? adapter.startScopedMining : adapter.stopScopedMining;
    if (!command) return;
    inFlight.current = true;
    setPending(true);
    setError(undefined);
    const generation = useMinerStore.getState().connectionGeneration;
    const isCurrentConnection = () => useMinerStore.getState().connectionGeneration === generation;
    let commanded = false;
    try {
      await authRetry({
        shouldRetry: () => !commanded && isCurrentConnection(),
        request: async (params) => {
          if (!isCurrentConnection()) return;
          await command.call(adapter, scope, params);
          commanded = true;
        },
        onSuccess: async () => {
          if (!isCurrentConnection()) return;
          const accept = beginStoreWrite("telemetry:current:hashboard,miner");
          const telemetry = await adapter.getCurrentTelemetry(["hashboard", "miner"]);
          if (accept()) useMinerStore.getState().telemetry.mergeTelemetry(telemetry, accept.requestId);
        },
        onError: () => {
          if (!isCurrentConnection()) return;
          setError(
            commanded
              ? "Command sent. Could not refresh tank status; waiting for the next update."
              : "Could not change tank power. Try again.",
          );
        },
      });
    } finally {
      inFlight.current = false;
      setPending(false);
    }
  };

  return { pending, error, setPower, supported: Boolean(adapter?.startScopedMining && adapter.stopScopedMining) };
};
