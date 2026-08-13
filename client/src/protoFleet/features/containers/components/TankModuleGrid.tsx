import type { ModuleActionType } from "./moduleActions";
import ModuleTile from "./ModuleTile";

/**
 * Composite health of a single tank module. Deliberately a two-state model
 * (unlike the fleet's five-state {@link SlotStatus}) — a container tank reads
 * as running vs needs-attention, not the full fleet status taxonomy.
 */
export type TankModuleState = "healthy" | "attention";

interface TankModuleGridProps {
  /** Column count. A 16-module tank renders as 8 × 2. */
  cols: number;
  rows: number;
  /** One entry per module bar, in row-major order. Missing entries read healthy. */
  modules: TankModuleState[];
  /** Human label for the tank, used to build per-module accessible labels. */
  label: string;
  /**
   * When supplied, each module bar becomes an action-menu trigger (View +
   * Blink LEDs / Reboot / Sleep), called with the module's row-major index and
   * the chosen action. When omitted the bars are plain status indicators.
   */
  onModuleAction?: (moduleIndex: number, action: ModuleActionType) => void;
}

/**
 * The distinct tank-detail visual for the container overview — the tank
 * analogue of RackHealthModule's RackDetailGrid, deliberately NOT the dense
 * fleet MiniRackGrid. Modules render as tall, generously spaced vertical bars:
 * a flat grey bar when healthy, and a three-cell orange bar (light body with a
 * brighter centre third) when the module needs attention. Each bar can carry a
 * popover action menu (see ModuleTile) when the caller wires onModuleAction.
 */
const TankModuleGrid = ({ cols, rows, modules, label, onModuleAction }: TankModuleGridProps) => {
  const total = cols * rows;

  return (
    <div
      data-testid="tank-module-grid"
      className="grid w-full gap-x-2.5 gap-y-3"
      style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}
    >
      {Array.from({ length: total }, (_, i) => {
        const state = modules[i] ?? "healthy";

        return (
          <ModuleTile
            key={i}
            state={state}
            label={`${label} module ${i + 1}`}
            onAction={onModuleAction ? (action) => onModuleAction(i, action) : undefined}
          />
        );
      })}
    </div>
  );
};

export default TankModuleGrid;
export type { TankModuleGridProps };
