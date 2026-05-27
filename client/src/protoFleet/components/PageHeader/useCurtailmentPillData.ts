import { useCallback, useEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { mapCurtailmentPillEvent } from "./curtailmentPillMapper";
import type { CurtailmentPillEvent } from "./curtailmentPillTypes";
import { curtailmentClient } from "@/protoFleet/api/clients";
import { GetActiveCurtailmentRequestSchema } from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { useAuthErrors } from "@/protoFleet/store";

export interface UseCurtailmentPillDataResult {
  activeEvent: CurtailmentPillEvent | null;
}

const POLL_INTERVAL_MS = 30_000;

export function useCurtailmentPillData(): UseCurtailmentPillDataResult {
  const { handleAuthErrors } = useAuthErrors();
  const [activeEvent, setActiveEvent] = useState<CurtailmentPillEvent | null>(null);

  const refreshActiveCurtailment = useCallback(async () => {
    try {
      const response = await curtailmentClient.getActiveCurtailment(create(GetActiveCurtailmentRequestSchema, {}));
      setActiveEvent(mapCurtailmentPillEvent(response.event));
    } catch (error) {
      handleAuthErrors({
        error,
        onError: () => setActiveEvent(null),
      });
    }
  }, [handleAuthErrors]);

  useEffect(() => {
    const initialRefreshId = window.setTimeout(() => {
      void refreshActiveCurtailment();
    }, 0);

    const intervalId = window.setInterval(() => {
      void refreshActiveCurtailment();
    }, POLL_INTERVAL_MS);

    return () => {
      window.clearTimeout(initialRefreshId);
      window.clearInterval(intervalId);
    };
  }, [refreshActiveCurtailment]);

  return { activeEvent };
}
