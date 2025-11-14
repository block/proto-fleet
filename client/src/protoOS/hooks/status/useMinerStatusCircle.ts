import { useMemo } from "react";
import { useMinerStore } from "@/protoOS/store";
import { statuses } from "@/shared/components/StatusCircle";
import type { StatusCircleProps } from "@/shared/components/StatusCircle";

/**
 * Returns the status circle configuration based on current state
 */
export const useMinerStatusCircle = (): StatusCircleProps["status"] => {
  const miningStatus = useMinerStore((state) => state.minerStatus.miningStatus);
  const errors = useMinerStore((state) => state.minerStatus.errors.errors);
  const isSleeping = /PoweringOff|Stopped/i.test(miningStatus || "");
  const isMining = /Mining/i.test(miningStatus || "");

  return useMemo(() => {
    if (isSleeping) {
      return statuses.sleeping;
    }

    // Treat all errors equally - show error status if any exist
    if (errors.length > 0) {
      return statuses.error;
    }

    if (isMining) {
      return statuses.normal;
    }

    // Use 'inactive' for idle state (not mining, no errors)
    return statuses.inactive;
  }, [errors, isSleeping, isMining]);
};
