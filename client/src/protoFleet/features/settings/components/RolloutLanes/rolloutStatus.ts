import { RolloutDeviceState, RolloutStatus } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

export type StatusTone = "neutral" | "progress" | "success" | "critical";

export const deviceStateLabels: Record<RolloutDeviceState, string> = {
  [RolloutDeviceState.UNSPECIFIED]: "",
  [RolloutDeviceState.PENDING]: "Pending",
  [RolloutDeviceState.UPDATING]: "Updating",
  [RolloutDeviceState.UPDATED]: "Updated",
};

export const rolloutStatusLabels: Record<RolloutStatus, string> = {
  [RolloutStatus.UNSPECIFIED]: "Unknown",
  [RolloutStatus.ACTIVE]: "In progress",
  [RolloutStatus.COMPLETED]: "Completed",
  [RolloutStatus.CANCELED]: "Canceled",
};

export const deviceStateTone = (state: RolloutDeviceState): StatusTone => {
  if (state === RolloutDeviceState.UPDATED) return "success";
  if (state === RolloutDeviceState.UPDATING) return "progress";
  return "neutral";
};

export const rolloutStatusTone = (status: RolloutStatus): StatusTone => {
  if (status === RolloutStatus.COMPLETED) return "success";
  if (status === RolloutStatus.ACTIVE) return "progress";
  return "neutral";
};
