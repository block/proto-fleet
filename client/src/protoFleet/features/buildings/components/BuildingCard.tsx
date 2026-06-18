import { type CSSProperties, type ReactNode, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import clsx from "clsx";

import { type BuildingRackHealth, type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { useBuildingStats } from "@/protoFleet/api/useBuildingStats";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { Ellipsis } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";
import { useInViewport } from "@/shared/hooks/useInViewport";
import { formatEfficiencyOrDash, formatHashrateOrDash, formatPowerMwOrDash } from "@/shared/utils/telemetryFormat";

interface BuildingCardProps {
  building: BuildingWithCounts;
}

// Rack-cell visual states. Five total: "unassigned" is owned by the renderer
// (no rack lives at this floor-plan position) and the four others come from
// the per-rack worst-state aggregation in GetBuildingStats.
type CellState = "unassigned" | "needsAttention" | "offline" | "sleeping" | "healthy";

// Priority rule for collapsing a rack's count buckets into a single cell
// state. Lives client-side (per #263 design discussion) so visual iteration
// doesn't churn the proto.
const cellStateForRack = (r: BuildingRackHealth): CellState => {
  if (r.brokenCount > 0) return "needsAttention";
  if (r.offlineCount > 0) return "offline";
  if (r.sleepingCount > 0) return "sleeping";
  return "healthy";
};

const CELL_CLASS: Record<CellState, string> = {
  unassigned: "border border-core-primary-10",
  needsAttention: "border border-core-primary-20 bg-intent-critical-10",
  offline: "border border-core-primary-20 bg-intent-warning-10",
  sleeping: "border border-core-primary-10",
  healthy: "border border-core-primary-20",
};

const CELL_DOT: Record<CellState, { kind: "none" } | { kind: "dot"; className: string } | { kind: "plus" }> = {
  unassigned: { kind: "plus" },
  needsAttention: { kind: "dot", className: "bg-intent-critical-fill" },
  offline: { kind: "dot", className: "bg-intent-warning-fill" },
  sleeping: { kind: "dot", className: "bg-core-primary-20" },
  healthy: { kind: "none" },
};

interface RackGridProps {
  aisles: number;
  racksPerAisle: number;
  cellStates: Record<string, CellState>;
  testId: string;
}

const cellKey = (aisle: number, position: number) => `${aisle}:${position}`;

// Visual constants. Cells normally render at MAX_CELL_PX (16px) with
// CELL_GAP_PX (4px) between them; once the natural grid width would
// overflow the container, the cells shrink uniformly via CSS `min()` so
// the floor plan keeps reading the same shape on narrow cards or wide
// buildings. MIN_CELL_PX floors the calc so pathological column counts
// (e.g. 100 cols × 300px card → calc() would resolve negative) still
// render visible cells; the grid overflows horizontally before going
// invisible.
const MAX_CELL_PX = 16;
const MIN_CELL_PX = 2;
const CELL_GAP_PX = 4;

const RackGrid = ({ aisles, racksPerAisle, cellStates, testId }: RackGridProps) => {
  if (aisles <= 0 || racksPerAisle <= 0) {
    return (
      <div className="text-200 text-text-primary-50" data-testid={`${testId}-empty`}>
        Floor plan not configured
      </div>
    );
  }

  const rows: { aisle: number; cells: CellState[] }[] = [];
  for (let a = 0; a < aisles; a++) {
    const cells: CellState[] = [];
    for (let p = 0; p < racksPerAisle; p++) {
      cells.push(cellStates[cellKey(a, p)] ?? "unassigned");
    }
    rows.push({ aisle: a, cells });
  }

  // Calc against 100% of the row width. `min(16px, ...)` clamps to the
  // fixed max when there's room; `max(MIN_CELL_PX, ...)` floors it so
  // pathological column counts (e.g. 100 cols on a 300px-wide card)
  // can't resolve to a negative width — the grid overflows horizontally
  // instead, which `justify-center` keeps visually centered. Total gap
  // is just (N-1) gaps between cells; outer breathing room comes from
  // the card's px-5 padding.
  //
  // Safety: the early return above guarantees racksPerAisle > 0 here, so
  // the `/ ${racksPerAisle}` divisor in the calc can never be zero. If
  // future refactors lift that guard, this calc will need a defensive
  // ceiling restored.
  const totalGapPx = (racksPerAisle - 1) * CELL_GAP_PX;
  // Use an intersection type to expose the custom property without a
  // blanket `as CSSProperties` cast — the css var is typed explicitly,
  // standard CSSProperties stays type-safe, and unrelated property
  // names can't be silently smuggled in.
  const gridStyle: CSSProperties & { "--cell-size": string } = {
    "--cell-size": `max(${MIN_CELL_PX}px, min(${MAX_CELL_PX}px, calc((100% - ${totalGapPx}px) / ${racksPerAisle})))`,
  };

  return (
    <div className="flex w-full max-w-full flex-col gap-1" data-testid={testId} style={gridStyle}>
      {rows.map((row) => (
        <div key={row.aisle} className="flex justify-center gap-1">
          {row.cells.map((state, p) => {
            const dot = CELL_DOT[state];
            return (
              <span
                key={p}
                aria-hidden
                data-cell-state={state}
                // `aspect-square` keeps the cell a square — `height: var(--cell-size)`
                // would resolve calc(100% - …) against the row's height (auto → 0)
                // instead of its width, collapsing the cell. Width drives the size,
                // aspect-ratio derives the height.
                className={clsx(
                  "flex aspect-square shrink-0 items-center justify-center rounded-[3px]",
                  CELL_CLASS[state],
                )}
                style={{ width: "var(--cell-size)" }}
              >
                {dot.kind === "dot" ? (
                  // Inner dot is sized as a percentage of the cell so it
                  // shrinks with the wrapper when the grid is space-constrained.
                  <span
                    className={clsx("block rounded-full", dot.className)}
                    style={{ width: "37.5%", height: "37.5%" }}
                  />
                ) : dot.kind === "plus" ? (
                  // The + glyph scales off the cell size for the same reason —
                  // 62.5% of cell = 10px at the 16px max, matching the prior
                  // text-[10px] design.
                  <span
                    className="leading-none text-core-primary-20"
                    style={{ fontSize: "calc(var(--cell-size) * 0.625)" }}
                  >
                    +
                  </span>
                ) : null}
              </span>
            );
          })}
        </div>
      ))}
    </div>
  );
};

interface StatProps {
  value: ReactNode;
  testId: string;
}

const Stat = ({ value, testId }: StatProps) => (
  <div className="px-3 py-3 text-300 text-text-primary-70 first:pl-5 last:pr-5" data-testid={testId}>
    {value}
  </div>
);

const BuildingCard = ({ building }: BuildingCardProps) => {
  const id = building.building?.id ?? 0n;
  const idText = id.toString();
  const label = building.building?.name ?? "(unnamed building)";
  const aisles = building.building?.aisles ?? 0;
  const racksPerAisle = building.building?.racksPerAisle ?? 0;

  // Viewport-gate the poll so an "All Sites" page rendering 50+ cards
  // doesn't fan out 50 GetBuildingStats RPCs every poll tick — only
  // currently-visible cards refresh. Cards retain their last-good stats
  // when scrolled offscreen (the hook keeps the snapshot when `enabled`
  // toggles), so re-revealing doesn't flash a skeleton.
  const cardRef = useRef<HTMLDivElement | null>(null);
  const isVisible = useInViewport(cardRef);

  // Poll so cards on /sites stay live as miners change state — without
  // this, the rollup numbers + cell colours drift until the next manual
  // refresh.
  const { stats, error: statsError } = useBuildingStats({
    buildingId: id,
    enabled: id !== 0n && isVisible,
    pollIntervalMs: POLL_INTERVAL_MS,
  });

  // Derive cell-state map keyed by aisle:position from the server's
  // rack_health list. Cells with no entry render unassigned.
  const cellStates = useMemo<Record<string, CellState>>(() => {
    if (!stats) return {};
    const acc: Record<string, CellState> = {};
    for (const r of stats.rackHealth) {
      if (r.aisleIndex === undefined || r.positionInAisle === undefined) continue;
      acc[cellKey(r.aisleIndex, r.positionInAisle)] = cellStateForRack(r);
    }
    return acc;
  }, [stats]);

  // Status summary segments. Each non-zero bucket renders as its own
  // "<N> <label>" clause; the floor plan up top already breaks the states
  // apart by colour so the summary just needs to surface counts. Order
  // (issues, offline, sleeping) matches the worst-first priority used by
  // the cell-state rule.
  const summarySegments: string[] = [];
  if (stats) {
    if (stats.brokenCount > 0) {
      summarySegments.push(`${stats.brokenCount} ${stats.brokenCount === 1 ? "issue" : "issues"}`);
    }
    if (stats.offlineCount > 0) {
      summarySegments.push(`${stats.offlineCount} offline`);
    }
    if (stats.sleepingCount > 0) {
      summarySegments.push(`${stats.sleepingCount} sleeping`);
    }
  }
  // "All healthy" only when the building actually has a rack assigned —
  // an empty floor plan shouldn't claim health it doesn't have.
  const hasAssignedRacks = (stats?.rackHealth.length ?? 0) > 0;
  const showAllHealthy = stats !== undefined && summarySegments.length === 0 && hasAssignedRacks;

  const navigate = useNavigate();
  const [menuOpen, setMenuOpen] = useState(false);
  useEscapeDismiss(menuOpen ? () => setMenuOpen(false) : undefined);
  const goToDetail = () => navigate(`/buildings/${idText}`);

  return (
    <div
      ref={cardRef}
      role="link"
      tabIndex={0}
      onClick={(e) => {
        if (menuOpen) return;
        if ((e.target as HTMLElement).closest("[data-popover='building-card-menu']")) return;
        goToDetail();
      }}
      onKeyDown={(e) => {
        if (e.key !== "Enter" && e.key !== " ") return;
        // Don't hijack keyboard activation when the focus is on the
        // ellipsis menu trigger or a popover menu item — mirror the
        // onClick guard above so keyboard users can navigate the menu.
        if ((e.target as HTMLElement).closest("[data-popover='building-card-menu'],[data-testid$='-menu-trigger']"))
          return;
        e.preventDefault();
        goToDetail();
      }}
      className="flex h-full cursor-pointer flex-col rounded-2xl bg-surface-5 transition-opacity hover:opacity-80"
      data-testid={`building-card-${idText}`}
    >
      <div className="flex items-center justify-between gap-2 px-5 pt-4">
        <span className="truncate text-emphasis-300 text-text-primary" data-testid={`building-card-${idText}-name`}>
          {label}
        </span>
        <div className="relative shrink-0">
          <button
            type="button"
            aria-label="Building actions"
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            onClick={(e) => {
              e.stopPropagation();
              setMenuOpen((prev) => !prev);
            }}
            className="flex size-8 items-center justify-center rounded-full text-text-primary-70 hover:bg-black/[0.06] dark:hover:bg-white/[0.06]"
            data-testid={`building-card-${idText}-menu-trigger`}
          >
            <Ellipsis width={iconSizes.small} />
          </button>
          {menuOpen ? (
            <BuildingCardMenu
              idText={idText}
              onDismiss={() => setMenuOpen(false)}
              onNavigate={(path) => {
                setMenuOpen(false);
                navigate(path);
              }}
            />
          ) : null}
        </div>
      </div>
      <div className="flex flex-1 flex-col items-center justify-center gap-3 px-5 py-6">
        <RackGrid
          aisles={aisles}
          racksPerAisle={racksPerAisle}
          cellStates={cellStates}
          testId={`building-card-${idText}-grid`}
        />
        <div
          className="flex min-h-5 items-center gap-2 text-200 text-text-primary-70"
          data-testid={`building-card-${idText}-status`}
        >
          {stats === undefined && statsError ? (
            // Surface stats-fetch failure so the card doesn't sit in a
            // permanent skeleton state. Card itself remains clickable; the
            // /buildings/:id page is the recovery path.
            <span className="text-intent-critical-text" data-testid={`building-card-${idText}-stats-error`}>
              Couldn&apos;t load stats
            </span>
          ) : stats === undefined ? (
            <SkeletonBar className="w-32" />
          ) : summarySegments.length > 0 ? (
            <>
              <span aria-hidden className="inline-block size-2 rounded-full bg-intent-critical-fill" />
              <span>{summarySegments.join(", ")}</span>
            </>
          ) : showAllHealthy ? (
            <span>All healthy</span>
          ) : null}
        </div>
      </div>
      <div className="grid grid-cols-4 divide-x divide-border-5 border-t border-border-5">
        <Stat
          testId={`building-card-${idText}-stat-hashrate`}
          value={
            stats === undefined ? (
              <SkeletonBar className="w-14" />
            ) : (
              formatHashrateOrDash(stats.hashrateReportingCount > 0 ? stats.totalHashrateThs : null)
            )
          }
        />
        <Stat
          testId={`building-card-${idText}-stat-efficiency`}
          value={
            stats === undefined ? (
              <SkeletonBar className="w-14" />
            ) : (
              formatEfficiencyOrDash(stats.efficiencyReportingCount > 0 ? stats.avgEfficiencyJth : null)
            )
          }
        />
        <Stat
          testId={`building-card-${idText}-stat-power`}
          value={
            stats === undefined ? (
              <SkeletonBar className="w-14" />
            ) : (
              formatPowerMwOrDash(stats.powerReportingCount > 0 ? stats.totalPowerKw : null)
            )
          }
        />
        <Stat
          testId={`building-card-${idText}-stat-racks`}
          value={(() => {
            // Prefer the polled stats.rackCount once available so the
            // footer stays in sync with rack assignments. Fall back to
            // the ListBuildings count during the first poll tick.
            const count = stats?.rackCount ?? Number(building.rackCount);
            return (
              <>
                {count} {count === 1 ? "rack" : "racks"}
              </>
            );
          })()}
        />
      </div>
    </div>
  );
};

interface BuildingCardMenuProps {
  idText: string;
  onDismiss: () => void;
  onNavigate: (path: string) => void;
}

const BuildingCardMenu = ({ idText, onDismiss, onNavigate }: BuildingCardMenuProps) => (
  <>
    <div
      className="fixed inset-0 z-20"
      role="presentation"
      onClick={(e) => {
        e.stopPropagation();
        onDismiss();
      }}
    />
    <div
      data-popover="building-card-menu"
      data-testid={`building-card-${idText}-menu`}
      role="menu"
      className="absolute top-full right-0 z-30 mt-1 w-44 rounded-xl border border-border-5 bg-surface-elevated-base py-1 shadow-300"
      onClick={(e) => e.stopPropagation()}
    >
      <BuildingCardMenuItem
        label="View details"
        testId={`building-card-${idText}-menu-details`}
        onClick={() => onNavigate(`/buildings/${idText}`)}
      />
      <BuildingCardMenuItem
        label="View racks"
        testId={`building-card-${idText}-menu-racks`}
        onClick={() => onNavigate(`/racks?building=${idText}`)}
      />
      <BuildingCardMenuItem
        label="View miners"
        testId={`building-card-${idText}-menu-miners`}
        onClick={() => onNavigate(`/miners?building=${idText}`)}
      />
    </div>
  </>
);

const BuildingCardMenuItem = ({ label, testId, onClick }: { label: string; testId: string; onClick: () => void }) => (
  <button
    type="button"
    role="menuitem"
    onClick={(e) => {
      e.stopPropagation();
      onClick();
    }}
    className="w-full px-4 py-2 text-left text-300 text-text-primary hover:bg-surface-5"
    data-testid={testId}
  >
    {label}
  </button>
);

export default BuildingCard;
