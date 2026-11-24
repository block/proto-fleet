import type { V2Slot } from "./generated/generatedApi";

/** Zero means unreported. Both gateways can serialize positions as strings. */
export const slotPosition = (slot: V2Slot) => {
  const configured = Number(slot.configuredLocation?.hashboardPosition ?? 0) || 0;
  return { configured, position: Number(slot.hashboard?.location?.hashboardPosition ?? 0) || configured };
};
