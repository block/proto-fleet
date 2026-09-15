import type { HashboardLayout } from "./types";
import { type AsicData, getAsicName, getContainerAsicLabel } from "@/protoOS/store";
import type { ModuleModel } from "@/protoOS/store/contract";
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
    rowClass: () => "",
    minWidth: "min-w-[800px]",
  },
  cell: {
    frame: "mb-1.5 grow basis-0 p-[2px] phone:truncate",
    fill: "",
    content: "py-3",
    metricValueVisibility: "always",
  },
  card: {
    title: (slot) => `Slot ${slot ?? "—"}`,
    showEfficiency: false,
    showInfoButton: true,
    showSlotIndicator: true,
    totalSlots: 9,
  },
  headerIcon: "dismiss",
  detailStats: [
    { metric: "maximumAsicTemperature", label: "Highest ASIC temp" },
    { metric: "averageAsicTemperature", label: "Avg ASIC temp" },
    { metric: "power", label: "Board power usage" },
    { metric: "hashrate", label: "Board hashrate" },
  ],
  detailStatsGrid: "grid-cols-4 phone:grid-cols-2",
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
    rowClass: (row) => (row > 0 && row % 2 === 0 ? "mt-5" : ""),
    // The cells' 40px minimum sets the grid's scroll width.
    minWidth: "",
  },
  // Cells grow to fill the row; compact cells keep their values in the popover.
  cell: {
    frame: "h-14 min-w-10 flex-1 p-0",
    fill: "h-full",
    content: "h-full justify-center py-1",
    metricValueVisibility: "when-wide",
  },
  card: {
    title: (slot) => `Board ${slot ?? "—"}`,
    showEfficiency: true,
    showInfoButton: false,
    showSlotIndicator: true,
    totalSlots: 3,
    previewRowHeight: 16,
  },
  headerIcon: "back",
  detailStats: [
    { metric: "maximumAsicTemperature", label: "High ASIC temp" },
    { metric: "averageAsicTemperature", label: "Avg ASIC temp" },
    { metric: "power", label: "Power usage" },
    { metric: "efficiency", label: "Efficiency" },
    { metric: "hashrate", label: "Total board hashrate" },
  ],
  detailStatsGrid: "grid-cols-5 phone:grid-cols-2",
  headerTitleSize: "text-heading-200",
};

export const LAYOUT_BY_MODULE_MODEL: Record<ModuleModel, HashboardLayout> = {
  r2: RIG_LAYOUT,
  ct1: CONTAINER_MODULE_LAYOUT,
  unknown: RIG_LAYOUT,
};
