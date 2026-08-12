import type { HashboardLayout } from "./types";
import { type AsicData, type DeviceType, getAsicName, getContainerAsicLabel } from "@/protoOS/store";
import { getRowLabel } from "@/shared/utils/utility";

/** Position-based serpentine label, or "" when the ASIC has no grid position. */
const containerLabel = (asic: AsicData): string =>
  asic.row !== undefined && asic.column !== undefined ? getContainerAsicLabel(asic.row, asic.column) : "";

/**
 * Proto rig: airflow runs front to rear, so inlet and outlet read at opposite
 * ends of a single rail and there is no bottom rail.
 */
export const RIG_LAYOUT: HashboardLayout = {
  labelCell: (asic, totalAsicCount) => (asic.index !== undefined ? getAsicName(totalAsicCount, asic.index) : ""),
  labelDetail: (asic) => `${getRowLabel(asic.row ?? 0)}${(asic.column ?? 0) + 1}`,
  rails: {
    top: {
      left: { label: "Front", reading: "inlet" },
      right: { label: "Rear", reading: "outlet" },
    },
  },
  grid: {
    frame: "w-full -space-y-[2px]",
    row: "gap-1.5",
    minWidth: "min-w-[800px]",
  },
  cell: {
    frame: "mb-1.5 grow basis-0 p-[2px] phone:truncate",
    fill: "",
    content: "py-3",
  },
  card: {
    showSlotIndicator: true,
  },
  headerTitleSize: "text-heading-300",
};

/**
 * Container module: airflow runs bottom to top, so the outlet reads across the
 * top rail and the inlet across a second rail below the grid. Each reading is
 * reinforced at both corners of its rail (see design).
 */
export const CONTAINER_MODULE_LAYOUT: HashboardLayout = {
  labelCell: containerLabel,
  labelDetail: containerLabel,
  rails: {
    top: {
      left: { label: "Top", reading: "outlet" },
      right: { label: "Top", reading: "outlet" },
    },
    bottom: {
      left: { label: "Bottom", reading: "inlet" },
      right: { label: "Bottom", reading: "inlet" },
    },
  },
  grid: {
    frame: "flex w-full flex-col gap-1",
    row: "gap-1",
    // No floor needed: the cells' own 56px minimum sets the grid's width.
    minWidth: "",
  },
  // Cells hold a 56px floor (min-w-14) and flex to fill the available width, so
  // the row scrolls horizontally when the viewport can't accommodate it.
  cell: {
    frame: "h-14 min-w-14 flex-1 p-0",
    fill: "h-full",
    content: "h-full justify-center py-1",
  },
  card: {
    showSlotIndicator: false,
    previewRowHeight: 16,
  },
  headerTitleSize: "text-heading-200",
};

export const LAYOUT_BY_DEVICE_TYPE: Record<DeviceType, HashboardLayout> = {
  rig: RIG_LAYOUT,
  containerModule: CONTAINER_MODULE_LAYOUT,
};
