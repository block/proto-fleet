import clsx from "clsx";

import type { ModuleActionType } from "./moduleActions";
import ModuleTile from "./ModuleTile";
import type { TankModuleState } from "./TankModuleGrid";
import { toTankRackView } from "./tankRackView";
import {
  type StatusBreakdownItem,
  StatusBreakdownPanel,
} from "@/protoFleet/features/dashboard/components/StatusBreakdownPanel";
import { Triangle } from "@/shared/assets/icons";

interface TankHealthModuleProps {
  /** Module-bar grid dimensions. A 16-module tank reads as 8 × 2. */
  cols: number;
  rows: number;
  /** One entry per module bar, row-major (matches the overview TankCard). */
  modules: TankModuleState[];
  /** Tank PDU state; when false every populated module reads offline. */
  on: boolean;
  /** Human label for the tank, used to build per-module accessible labels. */
  label: string;
  /**
   * When supplied each module bar becomes an action-menu trigger (View +
   * Blink LEDs / Reboot / Sleep), called with the module's row-major index and
   * the chosen action — the same menu the overview tank card exposes.
   */
  onModuleAction?: (moduleIndex: number, action: ModuleActionType) => void;
}

const moduleNumber = (index: number): string => String(index + 1).padStart(2, "0");

/**
 * Subtank-detail health panel (jmarr, 2026-08-10 — "reinforce the 3-segment
 * modules"). The tank analogue of RackHealthModule: the same elevated
 * two-column panel (grid on the left, status breakdown on the right), but the
 * left grid keeps the tank's own module language instead of switching to the
 * Fleet numbered-slot grid. Each module is the shared ModuleTile bar the
 * overview card uses — a flat grey bar when healthy, a bright center-third bar
 * when it needs attention, and a darker inert bar when the tank is powered off
 * — scaled up for the detail and labelled 01..N. The detail earns its name by
 * adding the per-module number and the status breakdown the compact card omits,
 * without abandoning the card's visual identity. Reusing ModuleTile keeps the
 * bar + action menu identical to the card (no drift), and toTankRackView
 * supplies the same tested counts the "tanks as racks" view uses.
 */
const TankHealthModule = ({ cols, rows, modules, on, label, onModuleAction }: TankHealthModuleProps) => {
  const total = cols * rows;
  const { hashingCount, needsAttentionCount, offlineCount } = toTankRackView({ cols, rows, modules, on });

  const breakdownItems: StatusBreakdownItem[] = [
    {
      key: "healthy",
      color: "--color-text-primary",
      label: "Healthy",
      percentageLabel: `${hashingCount} ${hashingCount === 1 ? "module" : "modules"}`,
      count: hashingCount,
    },
    {
      key: "needsAttention",
      color: "--color-intent-critical-fill",
      label: "Needs Attention",
      icon: <Triangle className="h-3 w-3" />,
      percentageLabel: `${needsAttentionCount} ${needsAttentionCount === 1 ? "module" : "modules"}`,
      count: needsAttentionCount,
    },
    {
      key: "offline",
      color: "--color-core-accent-fill",
      label: "Offline",
      percentageLabel: `${offlineCount} ${offlineCount === 1 ? "module" : "modules"}`,
      count: offlineCount,
    },
  ];

  return (
    <div
      className="flex w-full flex-col overflow-hidden rounded-xl bg-surface-elevated-base shadow-100 laptop:flex-row"
      data-testid="tank-health-module"
    >
      {/* Left: the tank's own 3-segment module bars, scaled up and numbered. */}
      <div className="flex w-full items-center justify-center p-6 laptop:w-1/2 laptop:p-10">
        {/* The wrapper carries the width so the grid's w-full has a real box to
            fill — otherwise, inside the centering flex parent, it would shrink
            to min-content and collapse the bars. */}
        <div
          className={clsx("w-full max-w-[520px]", !on && "opacity-40")}
          // Bars carry their own action menu; a click acts on that module.
          role="presentation"
        >
          <div
            className="grid w-full gap-x-3 gap-y-4"
            style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}
            data-testid="tank-health-grid"
          >
            {Array.from({ length: total }, (_, i) => (
              <div key={i} className="flex flex-col gap-2">
                <ModuleTile
                  state={on ? (modules[i] ?? "healthy") : "offline"}
                  label={`${label} module ${moduleNumber(i)}`}
                  onAction={on && onModuleAction ? (action) => onModuleAction(i, action) : undefined}
                />
                <span className="text-center text-200 text-text-primary-70 tabular-nums">{moduleNumber(i)}</span>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* Right: the shared status breakdown, in the tank's own state model. */}
      <StatusBreakdownPanel items={breakdownItems} className="w-full laptop:w-1/2" />
    </div>
  );
};

export default TankHealthModule;
export type { TankHealthModuleProps };
