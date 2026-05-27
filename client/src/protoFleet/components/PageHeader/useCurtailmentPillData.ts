import { useCallback, useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { mapCurtailmentPillEvent } from "./curtailmentPillMapper";
import type { CurtailmentPillEvent } from "./curtailmentPillTypes";
import { curtailmentClient } from "@/protoFleet/api/clients";
import { CURTAILMENT_CHANGED_EVENT } from "@/protoFleet/api/curtailmentEvents";
import { GetActiveCurtailmentRequestSchema } from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { isAbortError } from "@/protoFleet/api/requestErrors";
import { useAuthErrors } from "@/protoFleet/store";

export interface UseCurtailmentPillDataResult {
  activeEvent: CurtailmentPillEvent | null;
}

const POLL_INTERVAL_MS = 30_000;

export function useCurtailmentPillData(): UseCurtailmentPillDataResult {
  const { handleAuthErrors } = useAuthErrors();
  const [activeEvent, setActiveEvent] = useState<CurtailmentPillEvent | null>(null);
  const inFlightRefreshRef = useRef<Promise<void> | null>(null);
  const pendingFreshRefreshRef = useRef(false);

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
          const response = await curtailmentClient.getActiveCurtailment(create(GetActiveCurtailmentRequestSchema, {}), {
            signal,
          });
          if (signal.aborted) {
            return;
          }

          setActiveEvent(mapCurtailmentPillEvent(response.event));
        } catch (error) {
          if (isAbortError(error, signal)) {
            return;
          }

          handleAuthErrors({
            error,
            onError: () => setActiveEvent(null),
          });
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
    const intervalId = window.setInterval(refresh, POLL_INTERVAL_MS);
    window.addEventListener(CURTAILMENT_CHANGED_EVENT, refreshAfterCurtailmentChange);

    return () => {
      window.clearTimeout(initialRefreshId);
      window.clearInterval(intervalId);
      window.removeEventListener(CURTAILMENT_CHANGED_EVENT, refreshAfterCurtailmentChange);
      abortController.abort();
    };
  }, [refreshActiveCurtailment]);

  return { activeEvent };
}
