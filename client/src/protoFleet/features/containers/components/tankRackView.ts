import type { TankModuleState } from "./TankModuleGrid";
import type {
  NumberingOrigin,
  SlotHealthState,
} from "@/protoFleet/features/fleetManagement/components/RackDetailGrid/types";
import { slotNumberToRowCol } from "@/protoFleet/features/fleetManagement/utils/slotNumbering";

/**
 * Adapts a tank's module array (the container overview's two-state
 * healthy/needs-attention model) into the Fleet rack-detail view's inputs:
 * RackDetailGrid `slotStates` (keyed "row-col") plus the module counts
 * RackHealthModule renders in its breakdown panel. Pure (no DOM) so the mapping
 * — the point of "tanks as racks" — is unit-tested without jsdom.
 *
 * A tank powered off surfaces every rendered module as `offline` rather than
 * healthy/attention, mirroring how a de-energized rack reads in the fleet view.
 * Input modules keep TankModuleGrid's row-major order, and each module is placed
 * at the coordinate that the requested rack numbering origin displays as the
 * same one-based number. Module N therefore remains displayed slot N.
 */
export interface TankRackView {
  rows: number;
  cols: number;
  slotStates: Record<string, SlotHealthState>;
  /** Modules that are running and healthy. */
  hashingCount: number;
  /** Modules flagged needs-attention (0 when the tank is powered off). */
  needsAttentionCount: number;
  /** Populated modules that are dark because the tank's PDU is off. */
  offlineCount: number;
}

export interface TankRackViewInput {
  cols: number;
  rows: number;
  /** One entry per module bar, row-major (matches TankModuleGrid). */
  modules: TankModuleState[];
  /** Tank PDU state; when false every populated module reads offline. */
  on: boolean;
  /** Rack grid numbering to preserve module N as displayed slot N. */
  numberingOrigin?: NumberingOrigin;
}

export function toTankRackView({
  cols,
  rows,
  modules,
  on,
  numberingOrigin = "top-left",
}: TankRackViewInput): TankRackView {
  const total = cols * rows;
  const slotStates: Record<string, SlotHealthState> = {};

  let hashingCount = 0;
  let needsAttentionCount = 0;
  let offlineCount = 0;

  for (let i = 0; i < total; i++) {
    const { row, col } = slotNumberToRowCol(i + 1, rows, cols, numberingOrigin);
    const module = modules[i] ?? "healthy";

    let state: SlotHealthState;
    if (!on) {
      state = "offline";
      offlineCount++;
    } else if (module === "attention") {
      state = "needsAttention";
      needsAttentionCount++;
    } else {
      state = "healthy";
      hashingCount++;
    }

    slotStates[`${row}-${col}`] = state;
  }

  return { rows, cols, slotStates, hashingCount, needsAttentionCount, offlineCount };
}
