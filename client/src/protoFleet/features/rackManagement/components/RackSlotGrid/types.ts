import type { NumberingOrigin } from "@/protoFleet/features/rackManagement/utils/slotNumbering";

export type SlotVisualState = "empty" | "occupied" | "selected" | "selectedOccupied" | "dragOver" | "peerHover";

export type { NumberingOrigin };

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
  slotSize?: number;
}
