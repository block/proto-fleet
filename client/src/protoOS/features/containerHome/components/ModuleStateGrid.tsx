import { Link } from "react-router-dom";
import clsx from "clsx";
import type { ModuleGrid } from "../types";
import { StatusMarker } from "@/protoOS/features/tanks/TankModuleGrid";
import { statusColor } from "@/protoOS/features/tanks/tankStatusColors";
import { moduleStatuses, statusLabels } from "@/protoOS/features/tanks/tankView";

/** Every module at a glance: one row per tank, one cell per module, one bar per hashboard. */
export default function ModuleStateGrid({ grid }: { grid: ModuleGrid }) {
  if (!grid.tanks.length) return <p className="text-300 text-text-primary-50">No tanks reported.</p>;
  return (
    <div className="flex flex-col gap-5">
      <ul aria-label="Modules by state" className="flex flex-wrap gap-x-5 gap-y-2 text-200 text-text-primary-50">
        {moduleStatuses.map((status) => (
          <li key={status} className={clsx("flex items-center gap-2", grid.counts[status] > 0 && "text-text-primary")}>
            <StatusMarker status={status} />
            {statusLabels[status]}
            <span className="tabular-nums">{grid.counts[status]}</span>
          </li>
        ))}
      </ul>
      <div className="overflow-x-auto">
        <div
          className="grid min-w-[560px] items-center gap-x-1 gap-y-2"
          style={{ gridTemplateColumns: `auto repeat(${grid.columns}, minmax(0, 1fr))` }}
        >
          {grid.tanks.map((tank, row) => (
            <div key={tank.id} className="contents">
              <span className="pr-3 text-200 text-text-primary-50" style={{ gridRow: row + 1, gridColumn: 1 }}>
                {tank.label}
              </span>
              {tank.modules.map((module) => (
                <Link
                  key={module.id}
                  to={module.to}
                  aria-label={`${module.name}, ${statusLabels[module.state]}`}
                  title={`${module.name} · ${statusLabels[module.state]}`}
                  className="relative flex h-7 gap-px rounded-sm bg-core-primary-5 p-0.5 hover:bg-core-primary-10 focus-visible:outline-2 focus-visible:outline-core-accent-fill"
                  style={{ gridRow: row + 1, gridColumn: module.column + 1 }}
                >
                  {/* A module fault (a PSU, for example) does not change the hashboard bars. */}
                  {module.state === "attention" || module.state === "offline" || module.state === "sleeping" ? (
                    <span data-testid="module-state-marker" className="absolute -top-1.5 -right-1 flex scale-[0.65]">
                      <StatusMarker status={module.state} />
                    </span>
                  ) : null}
                  {module.boards.map((state, index) => (
                    <span
                      key={index}
                      aria-hidden="true"
                      className={clsx("h-full flex-1 rounded-xs", statusColor[state])}
                    />
                  ))}
                </Link>
              ))}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
