import { useEffect, useId, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import clsx from "clsx";

import { hashboardAttentionFill } from "./tankStatusColors";
import {
  boardStatus,
  hashboardLabel,
  moduleHashboards,
  type ModuleStatus,
  moduleStatus,
  statusLabels,
  tankGrid,
} from "./tankView";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import type {
  ContainerTopology,
  HashboardId,
  HashboardTelemetry,
  ModuleNode,
  TankNode,
} from "@/protoOS/store/contract";
import { moduleRouteValue, tankRouteValue } from "@/protoOS/topologyRouteParams";
import { ArrowRight } from "@/shared/assets/icons";
import Popover, { PopoverProvider, usePopover } from "@/shared/components/Popover";

const statusColor: Record<ModuleStatus, string> = {
  attention: "bg-intent-critical-fill",
  offline: "bg-core-accent-fill",
  sleeping: "bg-text-primary-30",
  healthy: "bg-text-primary",
  unknown: "border border-border-30",
};

export const StatusMarker = ({ status }: { status: ModuleStatus }) => (
  <span
    aria-hidden="true"
    className={clsx(
      "inline-block h-3.5 w-3.5 shrink-0",
      statusColor[status],
      status === "attention" ? "[clip-path:polygon(50%_0,100%_100%,0_100%)]" : "rounded-full",
    )}
  />
);

interface Props {
  tank: TankNode;
  topology: ContainerTopology;
  modules: ModuleNode[];
  telemetry: Map<HashboardId, HashboardTelemetry>;
  onViewModule: (module: ModuleNode) => void;
}

const TankModuleGridContent = ({ tank, topology, modules, telemetry, onViewModule }: Props) => {
  const gridId = useId();
  const { minerRoot } = useMinerHosting();
  const { triggerRef, setPopoverRenderMode } = usePopover();
  useEffect(() => setPopoverRenderMode("portal-scrolling"), [setPopoverRenderMode]);
  const [selected, setSelected] = useState<ModuleNode>();
  const grid = useMemo(() => tankGrid(tank, modules), [tank, modules]);
  const selectedBoards = selected
    ? moduleHashboards(selected, topology).sort(
        (a, b) => (a.location?.slot ?? Infinity) - (b.location?.slot ?? Infinity),
      )
    : [];

  return (
    <>
      <div className="min-w-0 overflow-x-auto p-1" aria-label="Tank module positions">
        <div
          className="grid gap-1.5"
          style={{
            gridTemplateColumns: `repeat(${grid.columns}, minmax(56px, 1fr))`,
            gridTemplateRows: `repeat(${grid.rows}, 142px)`,
          }}
        >
          {modules.map((module, index) => {
            const status = moduleStatus(module.hashboardIds.map((id) => telemetry.get(id)));
            const slot = module.location?.moduleSlot;
            const label = slot === undefined ? String(module.id) : String(slot).padStart(2, "0");
            const boards = moduleHashboards(module, topology);
            const bySlot = new Map(boards.map((board) => [board.location?.slot, board]));
            const position = grid.positions[index];
            const descriptionId = `${gridId}-${module.id}-health`;
            return (
              <div
                key={module.id}
                style={position ? { gridColumn: position.column, gridRow: position.row } : undefined}
              >
                <button
                  type="button"
                  data-tank-module-grid={gridId}
                  className="flex h-full w-full cursor-pointer flex-col items-center gap-2 rounded-lg border border-border-10 bg-surface-base px-3 py-3 hover:bg-surface-5 focus-visible:outline-2 focus-visible:outline-core-accent-fill"
                  aria-label={`Module ${label}, ${statusLabels[status]}`}
                  aria-describedby={descriptionId}
                  aria-expanded={selected?.id === module.id}
                  onClick={(event) => {
                    triggerRef.current = event.currentTarget.parentElement as HTMLDivElement;
                    setSelected(selected?.id === module.id ? undefined : module);
                  }}
                >
                  <span
                    className="flex h-[90px] w-9 overflow-hidden rounded-xs border border-border-10"
                    aria-hidden="true"
                  >
                    {[1, 2, 3].map((slot) => {
                      const board = bySlot.get(slot);
                      const reading = board ? telemetry.get(board.id) : undefined;
                      const state = boardStatus(reading);
                      return (
                        <span
                          key={slot}
                          className={clsx(
                            "h-full flex-1 border-r border-border-10 last:border-r-0",
                            state === "attention" && hashboardAttentionFill(reading?.reportedIssueCount),
                            !board && "bg-surface-5",
                          )}
                        />
                      );
                    })}
                  </span>
                  <span id={descriptionId} className="sr-only">
                    {boards.map((board) => {
                      const reading = telemetry.get(board.id);
                      const count = reading?.reportedIssueCount;
                      return `${hashboardLabel(board.location?.slot)} hashboard: ${statusLabels[boardStatus(reading)]}${
                        count ? `, ${count} reported ${count === 1 ? "issue" : "issues"}` : ""
                      }. `;
                    })}
                  </span>
                  <span className="flex items-center justify-center gap-1 font-mono text-200 text-text-primary">
                    {status === "attention" || status === "offline" || status === "sleeping" ? (
                      <StatusMarker status={status} />
                    ) : null}
                    {label}
                  </span>
                </button>
              </div>
            );
          })}
        </div>
      </div>
      {selected ? (
        <Popover
          key={selected.id}
          size="medium"
          position="top right"
          className="!p-2"
          closePopover={() => setSelected(undefined)}
          closeIgnoreSelectors={[`[data-tank-module-grid="${gridId}"]`]}
        >
          <div
            className="flex flex-col text-300 text-text-primary"
            aria-label={`Module ${selected.location?.moduleSlot ?? selected.id} actions`}
          >
            <button
              type="button"
              className="flex items-center gap-4 rounded-lg px-3 py-3 text-left hover:bg-surface-5"
              onClick={() => {
                setSelected(undefined);
                onViewModule(selected);
              }}
            >
              <ArrowRight />
              View module
            </button>
            {selectedBoards.map((board) => (
              <Link
                key={board.id}
                className="flex items-center gap-4 rounded-lg px-3 py-3 hover:bg-surface-5"
                to={`${minerRoot}/tanks/${tankRouteValue(tank)}/modules/${moduleRouteValue(selected, tank.moduleIds.indexOf(selected.id) + 1)}/${encodeURIComponent(board.identity.value)}`}
                onClick={() => setSelected(undefined)}
              >
                <span aria-hidden="true" className="flex h-6 w-5 gap-0.5 rounded-xs border border-border-10 p-0.5">
                  {[1, 2, 3].map((slot) => (
                    <span
                      key={slot}
                      className={clsx(
                        "w-1 border border-border-10",
                        slot === board.location?.slot && "bg-text-primary",
                      )}
                    />
                  ))}
                </span>
                View hashboard ({hashboardLabel(board.location?.slot)})
              </Link>
            ))}
          </div>
        </Popover>
      ) : null}
    </>
  );
};

const TankModuleGrid = (props: Props) => (
  <PopoverProvider>
    <TankModuleGridContent {...props} />
  </PopoverProvider>
);
export default TankModuleGrid;
