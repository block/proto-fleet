import type { CSSProperties } from "react";
import clsx from "clsx";
import { type ModuleGrid, type OverviewModuleStatus, overviewModuleStatuses } from "../types";
import styles from "./ModuleStateGrid.module.css";
import { statusLabels } from "@/protoOS/features/tanks/tankView";

import StatusCircle from "@/shared/components/StatusCircle";

const labels = { ...statusLabels, healthy: "Hashing" };

const StateMarker = ({ state, severity }: { state: OverviewModuleStatus; severity?: "critical" | "warning" }) => {
  if (state === "unknown")
    return (
      <span aria-hidden="true" className="font-mono text-text-primary-50">
        ?
      </span>
    );
  if (state === "attention")
    return (
      <span
        aria-hidden="true"
        className={clsx(
          "size-2.5 shrink-0 [clip-path:polygon(50%_0,100%_100%,0_100%)]",
          severity === "warning" ? "bg-intent-warning-fill" : "bg-intent-critical-fill",
        )}
      />
    );
  return (
    <StatusCircle
      status={state === "sleeping" ? "sleeping" : state === "offline" ? "warning" : "inactive"}
      variant="simple"
      width="w-2"
      removeMargin
    />
  );
};

const tankOrder = new Map([3, 2, 1, 6, 5, 4].map((slot, index) => [slot, index]));

/** Service-side tank layout: module 01 starts at the bottom left of each tank. */
export default function ModuleStateGrid({
  grid,
  onSelectModule,
}: {
  grid: ModuleGrid;
  onSelectModule?: (id: string) => void;
}) {
  if (!grid.tanks.length) return <p className="text-300 text-text-primary-50">No tanks reported.</p>;
  const tanks = [...grid.tanks].sort((a, b) => (a.slot ?? Infinity) - (b.slot ?? Infinity));
  const rows = new Map<number, typeof tanks>();
  tanks.forEach((tank, index) => {
    const row = Math.floor((tankOrder.get(tank.slot ?? 0) ?? index) / 3);
    rows.set(row, [...(rows.get(row) ?? []), tank]);
  });
  return (
    <div className={clsx("flex flex-col gap-5", styles.grid)}>
      <div aria-label="Tank layout" className="grid gap-4">
        {[...rows].map(([row, members]) => (
          <div key={row} className="grid grid-cols-1 gap-4 desktop:grid-cols-3">
            {members.map((tank) => (
              <section
                key={tank.id}
                aria-label={tank.label}
                className="min-w-0 rounded-xl bg-surface-elevated-base p-3 shadow-100 desktop:[grid-column:var(--tank-column)] desktop:row-start-1"
                style={
                  tank.slot && tank.slot <= 6
                    ? ({
                        "--tank-column": 3 - ((tank.slot - 1) % 3),
                      } as CSSProperties)
                    : undefined
                }
              >
                <h3 className="mb-3 text-heading-100 text-text-primary">{tank.label}</h3>
                <div className="overflow-x-auto p-0.5">
                  <div
                    aria-label={`${tank.label} module positions`}
                    className="grid gap-1"
                    style={{
                      gridTemplateColumns: `repeat(${tank.columns}, minmax(32px, 1fr))`,
                      gridTemplateRows: `repeat(${tank.rows}, 104px)`,
                    }}
                  >
                    {tank.modules.map((module) => {
                      const empty = module.boards.length === 3 && module.boards.every((board) => board === "empty");
                      const showState = module.state === "attention" || (!empty && module.state !== "healthy");
                      const label = empty
                        ? module.state === "attention"
                          ? `${labels[module.state]}, No boards mapped`
                          : "No boards mapped"
                        : labels[module.state];
                      return (
                        <button
                          key={module.id}
                          type="button"
                          onClick={() => onSelectModule?.(module.id)}
                          aria-label={`${module.name}, ${label}`}
                          title={`${module.name} · ${label} · ${module.boards.map((state, index) => `HB${index + 1}: ${state === "empty" ? "No board mapped" : state === "warning" ? "Warning" : labels[state]}`).join(" · ")}`}
                          className={clsx(
                            "flex h-full min-w-0 flex-col items-center justify-center gap-2 rounded-lg border px-0.5 py-2 hover:bg-surface-5 focus-visible:outline-2 focus-visible:outline-core-accent-fill",
                            styles.module,
                          )}
                          style={
                            module.position
                              ? { gridRow: module.position.row, gridColumn: module.position.column }
                              : undefined
                          }
                        >
                          <span className="flex h-16 w-6" aria-hidden="true">
                            {module.boards.map((state, index) => (
                              <span
                                key={index}
                                data-board-state={state}
                                className={clsx(
                                  "flex h-full min-w-0 flex-1 items-center justify-center",
                                  state !== "empty" && ["border", styles.board],
                                  state === "healthy" && styles.hashing,
                                  (state === "offline" || state === "unknown" || state === "sleeping") &&
                                    styles.inactive,
                                  state === "attention" && "bg-intent-critical-80",
                                  state === "warning" && "bg-intent-warning-80",
                                )}
                              >
                                {state === "offline" ? (
                                  <span className="size-1 shrink-0 rounded-full bg-intent-warning-fill" />
                                ) : null}
                                {state === "unknown" ? (
                                  <span className="font-mono text-[10px] text-text-primary-70">?</span>
                                ) : null}
                              </span>
                            ))}
                          </span>
                          <span className="flex items-center justify-center gap-0.5 font-mono text-300 leading-5 font-semibold text-text-primary">
                            {showState ? (
                              <span data-testid="module-state-marker" className="flex items-center">
                                <StateMarker state={module.state} severity={module.attentionSeverity} />
                              </span>
                            ) : null}
                            {module.label}
                          </span>
                        </button>
                      );
                    })}
                  </div>
                </div>
              </section>
            ))}
          </div>
        ))}
      </div>
      <ul aria-label="Modules by state" className="flex flex-wrap gap-x-5 gap-y-2 text-200 text-text-primary-50">
        {overviewModuleStatuses.map((status) => (
          <li key={status} className={clsx("flex items-center gap-2", grid.counts[status] > 0 && "text-text-primary")}>
            {status === "healthy" ? (
              <span className={clsx("size-2.5 border", styles.board, styles.hashing)} />
            ) : (
              <StateMarker state={status} />
            )}
            {status === "attention" ? (
              <>
                <StateMarker state="attention" severity="warning" />
                Error / warning
              </>
            ) : (
              labels[status]
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
