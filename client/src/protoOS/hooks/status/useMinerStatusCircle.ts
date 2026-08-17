import { useMemo } from "react";
import { useErrors, useMiningStatus } from "@/protoOS/store";
import { statuses } from "@/shared/components/StatusCircle";
import type { StatusCircleProps } from "@/shared/components/StatusCircle";

/**
 * Returns the status circle configuration based on current state
 */
export const useMinerStatusCircle = (): StatusCircleProps["status"] => {
  const miningStatus = useMiningStatus();
  const errors = useErrors();
  const isSleeping = miningStatus === "poweringOff" || miningStatus === "stopped";
  const isCurtailed = miningStatus === "curtailed";
  const isMining = miningStatus === "mining" || miningStatus === "degradedMining";

  return useMemo(() => {
    if (isSleeping) {
      return statuses.sleeping;
    }

    // Curtailed: mining paused by the curtailment service — not an error,
    // not hashing. Mirror sleeping's priority over errors.
    if (isCurtailed) {
      return statuses.inactive;
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
  }, [errors, isSleeping, isCurtailed, isMining]);
};
