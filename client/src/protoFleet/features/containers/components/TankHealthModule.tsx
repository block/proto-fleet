import clsx from "clsx";

import TankStatusMarker from "./TankStatusMarker";
import type { ModuleActionType } from "@/protoFleet/features/containers/components/moduleActions";
import ModuleTile from "@/protoFleet/features/containers/components/ModuleTile";
import type { ModuleSlot, TankModuleState } from "@/protoFleet/features/containers/components/TankModuleGrid";
import {
  type StatusBreakdownItem,
  StatusBreakdownPanel,
} from "@/protoFleet/features/dashboard/components/StatusBreakdownPanel";

interface TankHealthModuleProps {
  /** Module-bar grid dimensions. A 16-module tank reads as 8 × 2. */
  cols: number;
  rows: number;
  /** One entry per module bar, row-major (matches the overview TankCard). */
  modules: TankModuleState[];
  /** Tank PDU state; when false every populated module reads offline. */
  on: boolean | undefined;
  slots?: ModuleSlot[];
  onSelectModule?: (id: string) => void;
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

/** Numbered module bars and health counts for both collected and preview data. */
const TankHealthModule = ({
  cols,
  rows,
  modules,
  on,
  label,
  onModuleAction,
  slots,
  onSelectModule,
}: TankHealthModuleProps) => {
  const entries: ModuleSlot[] =
    slots ??
    Array.from({ length: cols * rows }, (_, i) => ({
      id: String(i),
      label: `${label} module ${moduleNumber(i)}`,
      state: on === false ? ("offline" as const) : (modules[i] ?? "unknown"),
      row: Math.floor(i / cols) + 1,
      column: (i % cols) + 1,
    }));
  const presentations: { key: TankModuleState; label: string; color: string }[] = [
    { key: "healthy", label: slots ? "Mining" : "Healthy", color: "--color-text-primary" },
    { key: "attention", label: "Needs Attention", color: "--color-intent-critical-fill" },
    { key: "offline", label: "Offline", color: "--color-core-accent-fill" },
    { key: "sleeping", label: "Sleeping", color: "--color-text-primary-30" },
  ];
  const breakdownItems: StatusBreakdownItem[] = presentations
    .map(({ key, label: stateLabel, color }) => {
      const count = entries.filter(
        (entry) => entry.state === key || (key === "attention" && entry.state === "isolated"),
      ).length;
      return {
        key,
        color,
        label: stateLabel,
        count,
        icon: <TankStatusMarker state={key} />,
        percentageLabel: `${count} ${count === 1 ? "module" : "modules"}`,
      };
    })
    .filter((item) => slots || item.count > 0 || ["healthy", "attention", "offline"].includes(item.key));
  const unknownCount = entries.filter((entry) => entry.state === "unknown").length;

  // Collected CT1 slots use the same service-side orientation as ProtoOS:
  // 01 starts at bottom left; missing modules never shift their neighbors.
  const columns = slots ? 8 : cols;
  const position = (entry: ModuleSlot, index: number) =>
    entry.position && entry.position > 0 ? entry.position : index + 1;
  const gridRows = Math.max(slots ? 2 : rows, Math.ceil(Math.max(0, ...entries.map(position)) / columns));

  return (
    <div
      className="flex w-full flex-col overflow-hidden rounded-xl bg-surface-elevated-base shadow-100 laptop:flex-row"
      data-testid="tank-health-module"
    >
      <div className="w-full min-w-0 p-6 laptop:w-3/5 laptop:p-10">
        {entries.length === 0 ? (
          <p>No modules reported.</p>
        ) : (
          <div className={clsx("overflow-x-auto p-1", on === false && "opacity-40")} aria-label="Tank module positions">
            <div
              className="grid gap-1.5"
              style={{
                gridTemplateColumns: `repeat(${columns}, minmax(56px, 1fr))`,
                gridTemplateRows: `repeat(${gridRows}, 142px)`,
              }}
              data-testid="tank-health-grid"
            >
              {entries.map((entry, i) => {
                const slot = position(entry, i);
                return (
                  <div
                    key={entry.id}
                    style={{
                      gridColumn: ((slot - 1) % columns) + 1,
                      gridRow: gridRows - Math.floor((slot - 1) / columns),
                    }}
                  >
                    <ModuleTile
                      state={entry.state}
                      label={entry.label}
                      number={String(slot).padStart(2, "0")}
                      onView={onSelectModule ? () => onSelectModule(entry.id) : undefined}
                      onAction={on && onModuleAction ? (action) => onModuleAction(i, action) : undefined}
                    />
                  </div>
                );
              })}
            </div>
          </div>
        )}
      </div>

      {/* Right: the shared status breakdown, in the tank's own state model. */}
      <div className="flex w-full flex-col laptop:w-2/5">
        <StatusBreakdownPanel
          items={breakdownItems}
          className={clsx("flex-1", unknownCount > 0 && "pb-3 laptop:pb-3")}
        />
        {unknownCount > 0 ? (
          <p className="flex items-center gap-2 px-6 pb-6 text-200 text-text-primary-70 laptop:px-10 laptop:pb-10">
            <TankStatusMarker state="unknown" />
            {unknownCount} {unknownCount === 1 ? "module" : "modules"} unknown
          </p>
        ) : null}
      </div>
    </div>
  );
};

export default TankHealthModule;
export type { TankHealthModuleProps };
