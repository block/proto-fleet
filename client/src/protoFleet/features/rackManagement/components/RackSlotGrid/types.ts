export type SlotVisualState = "empty" | "occupied" | "selected" | "selectedOccupied" | "dragOver" | "peerHover";

export type NumberingOrigin = "bottom-left" | "top-left" | "bottom-right" | "top-right";

export interface SlotData {
  slotNumber: number;
  state: SlotVisualState;
}

export interface RackSlotProps {
  slot: SlotData;
  slotSize?: number;
}

export interface RackSlotGridProps {
  rows: number;
  cols: number;
  slotStates?: Record<string, SlotVisualState>;
  numberingOrigin?: NumberingOrigin;
  slotsPerMiner?: number;
  slotSize?: number;
}
