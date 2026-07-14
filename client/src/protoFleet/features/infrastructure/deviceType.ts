import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";

export const formatDeviceType = (device: Pick<InfraDeviceItem, "deviceKind" | "fanCount">) => {
  if (device.deviceKind === "single_fan") return "Fan";
  if (device.fanCount > 1) return `Fan group (${device.fanCount} fans)`;
  return "Fan group";
};
