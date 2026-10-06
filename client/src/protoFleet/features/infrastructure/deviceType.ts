import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";

export const formatDeviceType = (device: Pick<InfraDeviceItem, "deviceKind" | "fanCount">) => {
  if (device.deviceKind === "single_fan") return "Fan";
  if (device.deviceKind === "fan_group") {
    return device.fanCount > 1 ? `Fan group (${device.fanCount} fans)` : "Fan group";
  }
  // A kind this client build doesn't know (newer server): show the raw
  // wire value rather than mislabeling it as a fan.
  return device.deviceKind;
};
