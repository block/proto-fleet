import type { AsicData } from "@/protoOS/store";

/**
 * Which hashboard rail temperature a rail end displays. Airflow direction
 * decides where each reading physically surfaces, so the descriptor names the
 * reading and the consumer resolves the value.
 */
export type RailReading = "inlet" | "outlet";

/** One end (left or right) of a rail running alongside the ASIC grid. */
export interface RailEnd {
  /** Physical edge name shown to the user, e.g. "Front", "Rear", "Top". */
  label: string;
  reading: RailReading;
}

/** A rail framing the ASIC grid, with an end at each side. */
export interface Rail {
  left: RailEnd;
  right: RailEnd;
}

/**
 * Everything about rendering a hashboard that varies by device type.
 *
 * One descriptor is resolved per device (see `layouts.ts`) and read through
 * `useHashboardLayout()`, so the components below it hold no device
 * conditionals. Supporting a new device type means adding a descriptor, not
 * editing components.
 */
export interface HashboardLayout {
  /**
   * Label for an ASIC in the grid, e.g. "F1" (serpentine) or "A3" (index).
   * Returns "" when the ASIC lacks the position/index data the label needs.
   */
  labelCell: (asic: AsicData, totalAsicCount: number) => string;

  /**
   * Label for an ASIC in its detail popover. Deliberately separate from
   * `labelCell`: the rig grid labels by ASIC index while the rig popover labels
   * by grid position, and those disagree. Container modules use the same
   * position-based label for both.
   */
  labelDetail: (asic: AsicData) => string;

  /** Rails framing the grid. `bottom` is omitted when airflow is horizontal. */
  rails: {
    top: Rail;
    bottom?: Rail;
  };

  /** Class names for the ASIC grid on the hashboard detail screen. */
  grid: {
    /** Wraps all rows. */
    frame: string;
    /** Applied to each row, alongside a base `flex`. */
    row: string;
    /** Minimum width of the scrollable grid area. */
    minWidth: string;
  };

  /** Class names for a single ASIC cell. */
  cell: {
    /** Outermost wrapper, which sizes the cell within its row. */
    frame: string;
    /** Applied to each nested element between the frame and the label. */
    fill: string;
    /** Innermost content column, which sets vertical padding. */
    content: string;
  };

  /** Presentation of the hashboard summary card in the diagnostics list. */
  card: {
    /** Rigs show a slot-position indicator; container modules do not. */
    showSlotIndicator: boolean;
    /** Pixel height per ASIC row in the preview heatmap; undefined = default. */
    previewRowHeight?: number;
  };

  /** Title size for the hashboard detail screen header. */
  headerTitleSize: string;
}
