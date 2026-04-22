import { CoolingStatusCoolingstatus, FanStatus } from "@/protoOS/api/generatedApi";
import { FanData } from "@/protoOS/store";

/**
 * Checks if all fans are disconnected based on RPM values from cooling API
 * @param fans - Array of fan status from cooling API
 * @returns true if fan data is present and every fan slot reports no RPM
 *
 * Note: We can't distinguish between a disconnected fan and a dead fan - both report RPM = 0.
 * This is because the GPIO tachometer circuit is simple and only reads RPM values.
 */
export const areAllFansDisconnected = (fans: (FanStatus | null)[] | null | undefined): boolean => {
  if (!fans) return false;

  return fans.length === 0 || fans.every((fan) => !fan || (fan.rpm ?? 0) === 0);
};

/**
 * Checks if fans are detected (running) while in immersion cooling mode
 * @param fans - Array of fan telemetry data
 * @param coolingMode - Current cooling mode from store
 * @returns true if fans are running in immersion mode
 */
export const areFansDetectedInImmersionMode = (
  fans: (FanData | undefined)[],
  coolingMode: CoolingStatusCoolingstatus["fan_mode"] | null,
): boolean => {
  const hasFansRunning = fans.some((fan) => fan && (fan.rpm?.latest?.value ?? 0) > 0);
  const isImmersionMode = coolingMode === "Off";

  return hasFansRunning && isImmersionMode;
};
