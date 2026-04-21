import { type RackCoolingType, RackOrderIndex } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import type { NumberingOrigin } from "@/protoFleet/features/rackManagement/utils/slotNumbering";

export type AssignmentMode = "manual" | "byName" | "byNetwork";

export interface RackFormData {
  label: string;
  zone: string;
  rows: number;
  columns: number;
  orderIndex: RackOrderIndex;
  coolingType: RackCoolingType;
}

export interface SelectedSlot {
  row: number;
  col: number;
  key: string; // "row-col" format
}

export function orderIndexToOrigin(orderIndex: RackOrderIndex): NumberingOrigin {
  const { BOTTOM_LEFT, TOP_LEFT, BOTTOM_RIGHT } = RackOrderIndex;
  if (orderIndex === BOTTOM_LEFT) return "bottom-left";
  if (orderIndex === TOP_LEFT) return "top-left";
  if (orderIndex === BOTTOM_RIGHT) return "bottom-right";
  return "top-right";
}

export function originLabel(origin: NumberingOrigin): string {
  switch (origin) {
    case "bottom-left":
      return "Bottom left";
    case "top-left":
      return "Top left";
    case "bottom-right":
      return "Bottom right";
    case "top-right":
      return "Top right";
  }
}
