/** Slot states aligned with FleetHealth dashboard metrics */
export type SlotStatus = "empty" | "healthy" | "needsAttention" | "offline" | "sleeping";

/** Rack-level status derived from its slots */
export type RackStatus = "healthy" | "needsAttention" | "offline" | "sleeping" | "mixed" | "empty";
