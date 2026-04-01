import type { NumberingOrigin } from "@/protoFleet/features/rackManagement/utils/slotNumbering";

export type SlotHealthState = "healthy" | "needsAttention" | "offline" | "sleeping" | "empty";

export type { NumberingOrigin };

export interface DetailSlotData {
  row: number;
  col: number;
  slotNumber: number;
  state: SlotHealthState;
}

export interface RackDetailSlotProps {
  slot: DetailSlotData;
  slotSize?: number;
  onEmptySlotClick?: (row: number, col: number) => void;
}

export interface RackDetailGridProps {
  rows: number;
  cols: number;
  slotStates?: Record<string, SlotHealthState>;
  numberingOrigin?: NumberingOrigin;
  slotSize?: number;
  onEmptySlotClick?: (row: number, col: number) => void;
}
