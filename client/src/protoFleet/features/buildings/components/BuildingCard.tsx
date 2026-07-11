import { type CSSProperties, type ReactNode, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { type BuildingRackHealth, type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { useBuildingStats } from "@/protoFleet/api/useBuildingStats";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import RowActionsMenu from "@/protoFleet/features/fleetManagement/components/RowActionsMenu";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { useInViewport } from "@/shared/hooks/useInViewport";
import { formatEfficiencyOrDash, formatHashrateOrDash, formatPowerMwOrDash } from "@/shared/utils/telemetryFormat";

interface BuildingCardProps {
  building: BuildingWithCounts;
  /**
   * Show the telemetry footer (hashrate / efficiency / power). Defaults to
   * true for the full fleet card; the dashboard renders a simplified card
   * without it.
   */
  showMetrics?: boolean;
}

type HeatBand = 0 | 1 | 2 | 3 | 4 | 5;

const issueRatio = (r: BuildingRackHealth): number => {
  const total = r.hashingCount + r.brokenCount + r.offlineCount + r.sleepingCount;
  if (total === 0) return 0;
  return (r.brokenCount + r.offlineCount + r.sleepingCount) / total;
};

const healthHeatBand = (ratio: number): HeatBand => {
  if (ratio <= 0) return 0;
  if (ratio < 0.05) return 1;
  if (ratio < 0.15) return 2;
  if (ratio < 0.3) return 3;
  if (ratio < 0.5) return 4;
  return 5;
};

const HEAT_CLASS: Record<HeatBand, string> = {
  0: "bg-core-primary-fill/10",
  1: "bg-intent-critical-fill/8",
  2: "bg-intent-critical-fill/18",
  3: "bg-intent-critical-fill/32",
  4: "bg-intent-critical-fill/50",
  5: "bg-intent-critical-fill/72",
};

interface RackGridProps {
  aisles: number;
  racksPerAisle: number;
  heatBands: Record<string, HeatBand>;
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

const RackGrid = ({ aisles, racksPerAisle, heatBands, testId }: RackGridProps) => {
  if (aisles <= 0 || racksPerAisle <= 0) {
    return (
      <div className="text-200 text-text-primary-50" data-testid={`${testId}-empty`}>
        Floor plan not configured
      </div>
    );
  }

  const rows: { aisle: number; bands: (HeatBand | null)[] }[] = [];
  for (let a = 0; a < aisles; a++) {
    const bands: (HeatBand | null)[] = [];
    for (let p = 0; p < racksPerAisle; p++) {
      const key = cellKey(a, p);
      bands.push(key in heatBands ? heatBands[key] : null);
    }
    rows.push({ aisle: a, bands });
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
          {row.bands.map((band, p) => (
            <span
              key={p}
              aria-hidden
              data-heat-band={band ?? "unassigned"}
              className={
                band !== null
                  ? `aspect-square shrink-0 rounded-[3px] ${HEAT_CLASS[band]}`
                  : "aspect-square shrink-0 rounded-[3px] border border-core-primary-10"
              }
              style={{ width: "var(--cell-size)" }}
            />
          ))}
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

const BuildingCard = ({ building, showMetrics = true }: BuildingCardProps) => {
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

  const heatBands = useMemo<Record<string, HeatBand>>(() => {
    if (!stats) return {};
    const acc: Record<string, HeatBand> = {};
    for (const r of stats.rackHealth) {
      if (r.aisleIndex === undefined || r.positionInAisle === undefined) continue;
      acc[cellKey(r.aisleIndex, r.positionInAisle)] = healthHeatBand(issueRatio(r));
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
  const suppressNextCardClickRef = useRef(false);
  const goToDetail = () => navigate(`/buildings/${idText}`);
  const suppressCardNavigationAfterDismiss = (target: EventTarget | null) => {
    if (menuOpen && target instanceof Element && !target.closest("[data-testid$='-menu-trigger']")) {
      suppressNextCardClickRef.current = true;
    }
  };
  const actions = useMemo(
    () => [
      {
        label: "View details",
        testId: `building-card-${idText}-menu-details`,
        onClick: () => navigate(`/buildings/${idText}`),
      },
      {
        label: "View racks",
        testId: `building-card-${idText}-menu-racks`,
        onClick: () => navigate(`/racks?building=${idText}`),
      },
      {
        label: "View miners",
        testId: `building-card-${idText}-menu-miners`,
        onClick: () => navigate(`/miners?building=${idText}`),
      },
    ],
    [idText, navigate],
  );

  return (
    <div
      ref={cardRef}
      role="link"
      tabIndex={0}
      onMouseDownCapture={(e) => suppressCardNavigationAfterDismiss(e.target)}
      onTouchStartCapture={(e) => suppressCardNavigationAfterDismiss(e.target)}
      onClick={(e) => {
        if (suppressNextCardClickRef.current) {
          suppressNextCardClickRef.current = false;
          return;
        }
        if (menuOpen) return;
        if ((e.target as HTMLElement).closest("[data-popover='building-card-menu']")) return;
        goToDetail();
      }}
      onKeyDown={(e) => {
        if (e.key !== "Enter" && e.key !== " ") return;
        if (menuOpen) return;
        // Don't hijack keyboard activation when the focus is on the
        // ellipsis menu trigger or a popover menu item — mirror the
        // onClick guard above so keyboard users can navigate the menu.
        if ((e.target as HTMLElement).closest("[data-popover='building-card-menu'],[data-testid$='-menu-trigger']"))
          return;
        e.preventDefault();
        goToDetail();
      }}
      className="flex h-full cursor-pointer flex-col rounded-2xl bg-surface-overlay transition-opacity hover:opacity-80"
      data-testid={`building-card-${idText}`}
    >
      <div className="flex items-center justify-between gap-2 px-5 pt-4">
        <span className="truncate text-emphasis-300 text-text-primary" data-testid={`building-card-${idText}-name`}>
          {label}
        </span>
        <div className="shrink-0" onClick={(e) => e.stopPropagation()}>
          <RowActionsMenu
            actions={actions}
            ariaLabel="Building actions"
            testIdPrefix={`building-card-${idText}-menu`}
            popoverTestId={`building-card-${idText}-menu`}
            triggerClassName="!size-8 !rounded-full !p-0 text-text-primary-70 hover:!bg-core-primary-5 hover:!opacity-100"
            onOpenChange={setMenuOpen}
          />
        </div>
      </div>
      <div className="flex flex-1 flex-col items-center justify-center gap-3 px-5 py-6">
        <RackGrid
          aisles={aisles}
          racksPerAisle={racksPerAisle}
          heatBands={heatBands}
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
      {showMetrics ? (
        <div className="grid grid-cols-3 divide-x divide-border-5 border-t border-border-5">
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
        </div>
      ) : null}
    </div>
  );
};

export default BuildingCard;
