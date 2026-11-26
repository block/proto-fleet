import { useEffect, useReducer } from "react";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

/** Keep CT1 observation ages current between polls, including after polling stops. */
export function usePsuObservationTime(enabled = true) {
  const { adapter } = useMinerHosting();
  return useObservationTime(enabled, adapter?.telemetryNow);
}

export function useObservationTime(enabled: boolean, now = Date.now) {
  const [, tick] = useReducer((value: number) => value + 1, 0);
  useEffect(() => {
    if (!enabled) return;
    const timer = setInterval(tick, 1_000);
    return () => clearInterval(timer);
  }, [enabled]);
  return now();
}
