import { useState } from "react";

import type { RolloutPlanConfig } from "./rolloutTypes";

/**
 * Convenience state hook for hosts (and stories) that don't own the rollout
 * plan state externally — bundles the config plus the schedule date/time so a
 * caller can wire {@link RolloutConfigModal} in a couple of lines.
 */
export function useRolloutConfigModalState(initial: RolloutPlanConfig) {
  const [config, setConfig] = useState<RolloutPlanConfig>(initial);
  const [startDate, setStartDate] = useState<Date | undefined>(new Date("2026-08-04T14:00:00"));
  const [startTime, setStartTime] = useState<string>("14:00");
  return { config, setConfig, startDate, setStartDate, startTime, setStartTime };
}
