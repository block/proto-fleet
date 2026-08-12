import clsx from "clsx";

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
}

/**
 * The distinct tank-detail visual for the container overview — the tank
 * analogue of RackHealthModule's RackDetailGrid, deliberately NOT the dense
 * fleet MiniRackGrid. Modules render as tall, generously spaced vertical bars:
 * a flat grey bar when healthy, and a three-cell orange bar (light body with a
 * brighter centre third) when the module needs attention.
 */
const TankModuleGrid = ({ cols, rows, modules }: TankModuleGridProps) => {
  const total = cols * rows;

  return (
    <div
      data-testid="tank-module-grid"
      className="grid w-full gap-x-2.5 gap-y-3"
      style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}
    >
      {Array.from({ length: total }, (_, i) => {
        const state = modules[i] ?? "healthy";
        const attention = state === "attention";

        return (
          <div
            key={i}
            data-testid="tank-module"
            data-module-state={state}
            className={clsx(
              "relative aspect-[4/7] overflow-hidden rounded-md",
              attention ? "bg-core-accent-50" : "bg-core-primary-10",
            )}
          >
            {/* Attention modules read as three equal-width cells: a bright centre
                third over the light body. */}
            {attention ? <span aria-hidden className="absolute inset-y-0 left-1/3 w-1/3 bg-core-accent-fill" /> : null}
          </div>
        );
      })}
    </div>
  );
};

export default TankModuleGrid;
export type { TankModuleGridProps };
