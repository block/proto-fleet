import { useCallback, useEffect, useMemo, useRef } from "react";

import { mapCurtailmentPillEvent } from "./curtailmentPillMapper";
import type { CurtailmentPillEvent } from "./curtailmentPillTypes";
import { refreshActiveCurtailmentData, useActiveCurtailmentEvent } from "@/protoFleet/api/activeCurtailmentData";
import { CURTAILMENT_CHANGED_EVENT } from "@/protoFleet/api/curtailmentEvents";
import { isAbortError } from "@/protoFleet/api/requestErrors";
import { useAuthErrors } from "@/protoFleet/store";

export interface UseCurtailmentPillDataResult {
  activeEvent: CurtailmentPillEvent | null;
}

const idlePollIntervalMs = 30_000;
const activeCurtailmentPollIntervalMs = 3_000;

export function useCurtailmentPillData(): UseCurtailmentPillDataResult {
  const { handleAuthErrors } = useAuthErrors();
  const activeCurtailmentEvent = useActiveCurtailmentEvent();
  const activeEvent = useMemo<CurtailmentPillEvent | null>(
    () => mapCurtailmentPillEvent(activeCurtailmentEvent),
    [activeCurtailmentEvent],
  );
  const inFlightRefreshRef = useRef<Promise<void> | null>(null);
  const pendingFreshRefreshRef = useRef(false);
  const pollIntervalMs = activeEvent === null ? idlePollIntervalMs : activeCurtailmentPollIntervalMs;

  const refreshActiveCurtailment = useCallback(
    (signal: AbortSignal, forceFresh = false): Promise<void> => {
      if (signal.aborted) {
        return Promise.resolve();
      }

      if (inFlightRefreshRef.current) {
        if (!forceFresh) {
          return inFlightRefreshRef.current;
        }

        pendingFreshRefreshRef.current = true;
        return inFlightRefreshRef.current.then(() => {
          if (!pendingFreshRefreshRef.current || signal.aborted) {
            return;
          }

          pendingFreshRefreshRef.current = false;
          return refreshActiveCurtailment(signal, true);
        });
      }

      pendingFreshRefreshRef.current = false;
      const refreshPromise = (async (): Promise<void> => {
        try {
          await refreshActiveCurtailmentData({ signal });
        } catch (error) {
          if (isAbortError(error, signal)) {
            return;
          }

          handleAuthErrors({ error });
        } finally {
          inFlightRefreshRef.current = null;
        }
      })();

      inFlightRefreshRef.current = refreshPromise;
      return refreshPromise;
    },
    [handleAuthErrors],
  );

  useEffect(() => {
    const abortController = new AbortController();

    const refresh = (): void => {
      void refreshActiveCurtailment(abortController.signal);
    };
    const refreshAfterCurtailmentChange = (): void => {
      void refreshActiveCurtailment(abortController.signal, true);
    };

    const initialRefreshId = window.setTimeout(refresh, 0);
    window.addEventListener(CURTAILMENT_CHANGED_EVENT, refreshAfterCurtailmentChange);

    return () => {
      window.clearTimeout(initialRefreshId);
      window.removeEventListener(CURTAILMENT_CHANGED_EVENT, refreshAfterCurtailmentChange);
      abortController.abort();
    };
  }, [refreshActiveCurtailment]);

  useEffect(() => {
    const abortController = new AbortController();
    const intervalId = window.setInterval(() => {
      void refreshActiveCurtailment(abortController.signal);
    }, pollIntervalMs);

    return () => {
      window.clearInterval(intervalId);
      abortController.abort();
    };
  }, [pollIntervalMs, refreshActiveCurtailment]);

  return { activeEvent };
}
