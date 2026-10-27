import { useEffect, useState } from "react";
import { isCurrentObservation } from "./moduleDetail";
import { miningGroupsClient } from "@/protoFleet/api/clients";
import type { GetStateResponse } from "@/protoFleet/api/generated/mininggroups/v1/mininggroups_pb";

// Read only while an action menu is open, rather than querying every inventory row.
export function useMiningGroupState(deviceIdentifier: string, active: boolean) {
  const key = active ? deviceIdentifier : "";
  const [previousKey, setPreviousKey] = useState(key);
  const [previousDevice, setPreviousDevice] = useState(deviceIdentifier);
  const [state, setState] = useState<GetStateResponse>();
  const [isFetching, setIsFetching] = useState(!!key);
  if (previousKey !== key || previousDevice !== deviceIdentifier) {
    setPreviousKey(key);
    setPreviousDevice(deviceIdentifier);
    setIsFetching(!!key);
    if (key || previousDevice !== deviceIdentifier) setState(undefined);
  }
  useEffect(() => {
    if (!key) return;
    const abort = new AbortController();
    let inFlight = false;
    const refresh = async () => {
      if (inFlight) return;
      inFlight = true;
      try {
        const next = await miningGroupsClient.getState({ deviceIdentifier: key }, { signal: abort.signal });
        if (!abort.signal.aborted) setState(next);
      } catch {
        if (!abort.signal.aborted) setState(undefined);
      } finally {
        inFlight = false;
        if (!abort.signal.aborted) setIsFetching(false);
      }
    };
    void refresh();
    const timer = setInterval(() => {
      if (inFlight) return;
      setIsFetching(true);
      void refresh();
    }, 4000);
    return () => {
      abort.abort();
      clearInterval(timer);
    };
  }, [key]);
  return { state, isFetching };
}

export function canEditHashboardPools(state: GetStateResponse | undefined) {
  return (
    !!state?.supported &&
    state.enabled &&
    isCurrentObservation(state.observedAt, Date.now()) &&
    !["pending", "applying", "blocked"].includes(state.status)
  );
}
