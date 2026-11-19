import { useMemo } from "react";
import { getScopeHealth, type HealthScope } from "./scopeHealth";
import { usePsuObservationTime } from "@/protoOS/hooks/usePsuObservationTime";
import { useDetailedErrors } from "@/protoOS/store/hooks/useMinerStatus";
import useMinerStore from "@/protoOS/store/useMinerStore";

export function useScopeHealth(scope: HealthScope) {
  const hardware = useMinerStore((state) => state.hardware);
  const telemetry = useMinerStore((state) => state.telemetry);
  const errors = useDetailedErrors();
  const now = usePsuObservationTime();
  return useMemo(
    () => getScopeHealth(scope, hardware, telemetry, errors, now),
    [scope, hardware, telemetry, errors, now],
  );
}
