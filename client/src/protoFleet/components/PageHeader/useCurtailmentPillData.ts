import { useCallback, useEffect, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { curtailmentClient } from "@/protoFleet/api/clients";
import { isUnimplementedConnectError } from "@/protoFleet/api/connectErrorHelpers";
import {
  getCurtailmentEstimatedReductionKw,
  getCurtailmentScopeLabel,
  getCurtailmentSelectedMinerCount,
  isActiveCurtailmentEventState,
  mapCurtailmentEventState,
} from "@/protoFleet/api/curtailmentEventMappers";
import { subscribeToCurtailmentChanges } from "@/protoFleet/api/curtailmentNotifications";
import {
  type CurtailmentEvent,
  GetActiveCurtailmentRequestSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import type { CurtailmentPillEvent } from "@/protoFleet/components/PageHeader/curtailmentPillTypes";
import { useAuthErrors } from "@/protoFleet/store";

export interface UseCurtailmentPillDataResult {
  activeEvent: CurtailmentPillEvent | null;
  refreshActiveCurtailment: () => Promise<void>;
}

const POLL_INTERVAL_MS = 30_000;

function mapPillEvent(event: CurtailmentEvent): CurtailmentPillEvent | null {
  const state = mapCurtailmentEventState(event.state);

  if (!isActiveCurtailmentEventState(state)) {
    return null;
  }

  return {
    reason: event.reason,
    state,
    scopeLabel: getCurtailmentScopeLabel(event),
    selectedMiners: getCurtailmentSelectedMinerCount(event),
    estimatedReductionKw: getCurtailmentEstimatedReductionKw(event),
  };
}

export function useCurtailmentPillData(): UseCurtailmentPillDataResult {
  const { handleAuthErrors } = useAuthErrors();
  const [activeEvent, setActiveEvent] = useState<CurtailmentPillEvent | null>(null);

  const refreshActiveCurtailment = useCallback(async () => {
    try {
      const response = await curtailmentClient.getActiveCurtailment(create(GetActiveCurtailmentRequestSchema, {}));
      setActiveEvent(response.event ? mapPillEvent(response.event) : null);
    } catch (error) {
      if (isUnimplementedConnectError(error)) {
        setActiveEvent(null);
        return;
      }

      handleAuthErrors({ error });
    }
  }, [handleAuthErrors]);

  useEffect(() => {
    const initialRefreshId = window.setTimeout(() => {
      void refreshActiveCurtailment();
    }, 0);
    const intervalId = window.setInterval(() => {
      void refreshActiveCurtailment();
    }, POLL_INTERVAL_MS);
    const unsubscribe = subscribeToCurtailmentChanges(() => {
      void refreshActiveCurtailment();
    });

    return () => {
      window.clearTimeout(initialRefreshId);
      window.clearInterval(intervalId);
      unsubscribe();
    };
  }, [refreshActiveCurtailment]);

  return useMemo(
    () => ({
      activeEvent,
      refreshActiveCurtailment,
    }),
    [activeEvent, refreshActiveCurtailment],
  );
}
