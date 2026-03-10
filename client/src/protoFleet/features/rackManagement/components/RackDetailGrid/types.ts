export type SlotHealthState = "healthy" | "needsAttention" | "offline" | "sleeping" | "empty";

export type NumberingOrigin = "bottom-left" | "top-left" | "bottom-right" | "top-right";

export interface DetailSlotData {
  slotNumber: number;
  state: SlotHealthState;
}

export interface RackDetailSlotProps {
  slot: DetailSlotData;
  slotSize?: number;
}

export interface RackDetailGridProps {
  rows: number;
  cols: number;
  slotStates?: Record<string, SlotHealthState>;
  numberingOrigin?: NumberingOrigin;
  slotsPerMiner?: number;
  slotSize?: number;
}
