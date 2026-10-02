import type { ModuleActionType } from "./moduleActions";
import ModuleTile from "./ModuleTile";

/** Module health and operational states used by Fleet container views. */
export type TankModuleState = "healthy" | "attention" | "offline" | "sleeping" | "isolated" | "unknown";

export interface ModuleSlot {
  id: string;
  label: string;
  state: TankModuleState;
  row: number;
  column: number;
}
interface TankModuleGridProps {
  slots?: ModuleSlot[];
  onSelectModule?: (id: string) => void;
  /** Column count. A 16-module tank renders as 8 × 2. */
  cols: number;
  rows: number;
  /** One entry per module bar, in row-major order. Missing entries read unknown. */
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
const TankModuleGrid = ({ cols, rows, modules, label, onModuleAction, slots, onSelectModule }: TankModuleGridProps) => {
  const total = cols * rows;

  return (
    <div
      data-testid="tank-module-grid"
      className="grid w-full gap-x-2.5 gap-y-3"
      style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}
    >
      {slots
        ? slots.map((slot) => (
            <div key={slot.id} style={{ gridColumn: slot.column, gridRow: slot.row }}>
              <ModuleTile
                state={slot.state}
                label={slot.label}
                onView={onSelectModule ? () => onSelectModule(slot.id) : undefined}
              />
            </div>
          ))
        : Array.from({ length: total }, (_, i) => {
            const state = modules[i] ?? "unknown";

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
