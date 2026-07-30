import { type MouseEvent as ReactMouseEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import clsx from "clsx";
import { createPortal } from "react-dom";

import { type BuildingRackHealth } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { HealthBar } from "@/protoFleet/components/HealthBar";
import { ChevronDown } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import SegmentedControl from "@/shared/components/SegmentedControl";

export interface BuildingRackGridProps {
  rackHealth: BuildingRackHealth[];
  aisles: number;
  racksPerAisle: number;
  testId?: string;
}

type SortMode = "name" | "issues";

const TILE_MIN_PX = 100;
const TILE_GAP_PX = 8;
type MinimapStatus = "healthy" | "issue" | "offline" | "sleeping" | "empty";

const MINIMAP_COLORS: Record<MinimapStatus, string> = {
  healthy: "bg-text-primary",
  issue: "bg-intent-critical-fill",
  offline: "bg-intent-warning-fill",
  sleeping: "bg-core-primary-20",
  empty: "bg-transparent",
};

const SORT_SEGMENTS = [
  { key: "name", title: "Layout" },
  { key: "issues", title: "Issues" },
];

const rackIssueCount = (r: BuildingRackHealth) => r.brokenCount + r.offlineCount;

const worstStatus = (r: BuildingRackHealth): MinimapStatus => {
  if (r.brokenCount > 0) return "issue";
  if (r.offlineCount > 0) return "offline";
  if (r.sleepingCount > 0) return "sleeping";
  if (r.hashingCount > 0) return "healthy";
  return "empty";
};

const BuildingRackGrid = ({
  rackHealth,
  aisles,
  racksPerAisle,
  testId = "building-rack-grid",
}: BuildingRackGridProps) => {
  const navigate = useNavigate();
  const containerRef = useRef<HTMLDivElement>(null);
  const [responsiveCols, setResponsiveCols] = useState(9);
  const [sortMode, setSortMode] = useState<SortMode>("name");
  const [page, setPage] = useState(0);

  const hasLayout = aisles > 0 && racksPerAisle > 0;

  const handleSortChange = useCallback((key: string) => {
    setSortMode(key as SortMode);
    setPage(0);
  }, []);

  const hasRacks = rackHealth.length > 0;

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const observer = new ResizeObserver(([entry]) => {
      const w = entry.contentRect.width;
      setResponsiveCols(Math.max(3, Math.min(10, Math.floor((w + TILE_GAP_PX) / (TILE_MIN_PX + TILE_GAP_PX)))));
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [hasRacks]);

  // 2D floor plan: floorPlan[aisle][position] = rack | null
  const floorPlan = useMemo(() => {
    if (!hasLayout) return null;
    const grid: (BuildingRackHealth | null)[][] = Array.from({ length: aisles }, () =>
      Array.from<BuildingRackHealth | null>({ length: racksPerAisle }).fill(null),
    );
    for (const rack of rackHealth) {
      const a = rack.aisleIndex;
      const p = rack.positionInAisle;
      if (a != null && p != null && a < aisles && p < racksPerAisle) {
        grid[a][p] = rack;
      }
    }
    return grid;
  }, [rackHealth, hasLayout, aisles, racksPerAisle]);

  // Racks with no floor-plan position — shown as an extra section in Name view
  const unplacedRacks = useMemo(() => {
    if (!hasLayout) return [];
    return rackHealth
      .filter((r) => r.aisleIndex == null || r.positionInAisle == null)
      .sort((a, b) => a.rackLabel.localeCompare(b.rackLabel, undefined, { numeric: true }));
  }, [rackHealth, hasLayout]);

  // Label-sorted flat list (Name tab fallback when no floor plan)
  const nameSorted = useMemo(() => {
    const arr = [...rackHealth];
    arr.sort((a, b) => a.rackLabel.localeCompare(b.rackLabel, undefined, { numeric: true }));
    return arr;
  }, [rackHealth]);

  // Issue-sorted flat list
  const issueSorted = useMemo(() => {
    const arr = [...rackHealth];
    arr.sort(
      (a, b) =>
        rackIssueCount(b) - rackIssueCount(a) || a.rackLabel.localeCompare(b.rackLabel, undefined, { numeric: true }),
    );
    return arr;
  }, [rackHealth]);

  const isNameSort = sortMode === "name";
  const useFloorPlan = isNameSort && floorPlan !== null;

  // Floor plan: paginate through position columns; all aisles always visible
  // Issue sort: all racks in a flat grid, no pagination
  const cols = useFloorPlan ? Math.min(racksPerAisle, responsiveCols) : responsiveCols;
  const totalPages = useFloorPlan ? Math.max(1, Math.ceil(racksPerAisle / cols)) : 1;
  const safePage = Math.min(page, totalPages - 1);

  const posStart = useFloorPlan ? safePage * cols : 0;
  const posEnd = useFloorPlan ? Math.min(posStart + cols, racksPerAisle) : 0;

  const pageRows: (BuildingRackHealth | null)[][] = useMemo(() => {
    if (useFloorPlan) {
      return floorPlan!.map((aisle) => aisle.slice(posStart, posEnd));
    }
    const source = isNameSort ? nameSorted : issueSorted;
    const rows: BuildingRackHealth[][] = [];
    for (let i = 0; i < source.length; i += responsiveCols) {
      rows.push(source.slice(i, i + responsiveCols));
    }
    return rows;
  }, [useFloorPlan, floorPlan, posStart, posEnd, responsiveCols, isNameSort, nameSorted, issueSorted]);

  const showPagination = useFloorPlan && totalPages > 1;

  // Hover popover state
  const [hoverInfo, setHoverInfo] = useState<{
    rack: BuildingRackHealth;
    x: number;
    y: number;
  } | null>(null);
  const popoverRef = useRef<HTMLDivElement>(null);

  const handleTileMouseMove = useCallback((e: ReactMouseEvent, rack: BuildingRackHealth) => {
    setHoverInfo({ rack, x: e.clientX, y: e.clientY });
  }, []);

  const handleTileMouseLeave = useCallback(() => setHoverInfo(null), []);

  const popoverStyle = useMemo(() => {
    if (!hoverInfo) return undefined;
    const popoverH = popoverRef.current?.offsetHeight ?? 60;
    return {
      left: hoverInfo.x + 12,
      top: hoverInfo.y - popoverH - 12,
    };
  }, [hoverInfo]);

  if (rackHealth.length === 0) {
    return (
      <div
        className="rounded-2xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70"
        data-testid={`${testId}-empty`}
      >
        No racks in this building yet.
      </div>
    );
  }

  const visibleCols = useFloorPlan ? posEnd - posStart : cols;

  return (
    <div
      ref={containerRef}
      className="flex flex-col gap-4 rounded-xl bg-surface-elevated-base p-10 shadow-100 phone:p-6"
      data-testid={testId}
    >
      {/* Sort control */}
      <SegmentedControl segments={SORT_SEGMENTS} initialSegmentKey="name" onSelect={handleSortChange} />

      {/* Rack tile grid */}
      <div className="flex flex-col gap-2">
        {pageRows.map((row, ri) => (
          <div
            key={ri}
            className="grid"
            style={{ gridTemplateColumns: `repeat(${visibleCols}, 1fr)`, gap: `${TILE_GAP_PX}px` }}
          >
            {row.map((rack, ci) =>
              rack ? (
                <div
                  key={rack.rackId.toString()}
                  role="link"
                  tabIndex={0}
                  onClick={() => navigate(`/racks/${rack.rackId.toString()}`)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      navigate(`/racks/${rack.rackId.toString()}`);
                    }
                  }}
                  onMouseMove={(e) => handleTileMouseMove(e, rack)}
                  onMouseLeave={handleTileMouseLeave}
                  className="flex cursor-pointer flex-col items-center gap-2.5 rounded-xl bg-surface-overlay p-4 transition-opacity duration-[120ms] hover:opacity-[0.82]"
                  data-testid={`${testId}-tile-${rack.rackLabel}`}
                >
                  <span className="text-emphasis-300 text-text-primary">{rack.rackLabel}</span>
                  <div className="w-full">
                    <HealthBar
                      healthy={rack.hashingCount}
                      needsAttention={rack.brokenCount}
                      offline={rack.offlineCount}
                      sleeping={rack.sleepingCount}
                      testId={`${testId}-bar-${rack.rackLabel}`}
                    />
                  </div>
                </div>
              ) : (
                <div
                  key={`empty-${ci}`}
                  className="rounded-xl bg-surface-5/50"
                  style={{ minHeight: `${TILE_MIN_PX}px` }}
                  data-testid={`${testId}-empty-${ri}-${ci}`}
                />
              ),
            )}
          </div>
        ))}
      </div>

      {/* Unplaced racks — shown when in Name view with floor plan */}
      {useFloorPlan && unplacedRacks.length > 0 ? (
        <div className="mt-4">
          <div className="mb-2 text-200 text-text-primary-70">Unplaced ({unplacedRacks.length})</div>
          <div className="grid gap-2" style={{ gridTemplateColumns: `repeat(${cols}, minmax(${TILE_MIN_PX}px, 1fr))` }}>
            {unplacedRacks.map((rack) => (
              <div
                key={rack.rackId}
                className="cursor-pointer rounded-xl bg-surface-overlay p-4 transition-opacity duration-[120ms] hover:opacity-[0.82]"
                onClick={() => navigate(`/racks/${rack.rackId}`)}
                data-testid={`${testId}-unplaced-${rack.rackLabel}`}
              >
                <div className="mb-2 truncate text-emphasis-300 text-text-primary">{rack.rackLabel}</div>
                <HealthBar
                  healthy={rack.hashingCount}
                  needsAttention={rack.brokenCount}
                  offline={rack.offlineCount}
                  sleeping={rack.sleepingCount}
                  testId={`${testId}-bar-${rack.rackLabel}`}
                />
              </div>
            ))}
          </div>
        </div>
      ) : null}

      {/* Footer: minimap + pagination */}
      {showPagination ? (
        <div className="flex items-stretch justify-between gap-4">
          <Minimap
            floorPlan={floorPlan!}
            racksPerAisle={racksPerAisle}
            colsPerPage={cols}
            posStart={posStart}
            onPageSelect={setPage}
            testId={`${testId}-minimap`}
          />
          <div className="flex shrink-0 items-center gap-3 text-300 text-text-primary-70">
            <span>
              {posStart + 1}&ndash;{posEnd} of {racksPerAisle} positions
            </span>
            <button
              type="button"
              disabled={safePage === 0}
              onClick={() => setPage((p) => Math.max(0, p - 1))}
              className="flex size-8 items-center justify-center rounded-full border border-border-5 text-text-primary hover:bg-surface-5 disabled:opacity-35"
              aria-label="Previous page"
              data-testid={`${testId}-prev`}
            >
              <ChevronDown width={iconSizes.xSmall} className="rotate-90" />
            </button>
            <button
              type="button"
              disabled={safePage >= totalPages - 1}
              onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
              className="flex size-8 items-center justify-center rounded-full border border-border-5 text-text-primary hover:bg-surface-5 disabled:opacity-35"
              aria-label="Next page"
              data-testid={`${testId}-next`}
            >
              <ChevronDown width={iconSizes.xSmall} className="-rotate-90" />
            </button>
          </div>
        </div>
      ) : null}

      {/* Cursor-following hover popover */}
      {hoverInfo
        ? createPortal(
            <div
              ref={popoverRef}
              className="pointer-events-none fixed z-50 rounded-2xl border border-border-5 bg-surface-elevated-base px-5 py-4 shadow-300"
              style={popoverStyle}
              data-testid={`${testId}-popover`}
            >
              <div className="mb-2 text-emphasis-300 text-text-primary">{hoverInfo.rack.rackLabel}</div>
              {hoverInfo.rack.hashingCount > 0 ? (
                <div className="flex items-center gap-2 text-300 text-text-primary">
                  <span className="inline-block size-2 shrink-0 rounded-full bg-text-primary" />
                  {hoverInfo.rack.hashingCount} healthy
                </div>
              ) : null}
              {hoverInfo.rack.brokenCount > 0 ? (
                <div className="mt-1.5 flex items-center gap-2 text-300 text-text-primary">
                  <span className="inline-block size-2 shrink-0 rounded-full bg-intent-critical-fill" />
                  {hoverInfo.rack.brokenCount} need attention
                </div>
              ) : null}
              {hoverInfo.rack.offlineCount > 0 ? (
                <div className="mt-1.5 flex items-center gap-2 text-300 text-text-primary">
                  <span className="inline-block size-2 shrink-0 rounded-full bg-intent-warning-fill" />
                  {hoverInfo.rack.offlineCount} offline
                </div>
              ) : null}
              {hoverInfo.rack.sleepingCount > 0 ? (
                <div className="mt-1.5 flex items-center gap-2 text-300 text-text-primary">
                  <span className="inline-block size-2 shrink-0 rounded-full bg-core-primary-20" />
                  {hoverInfo.rack.sleepingCount} sleeping
                </div>
              ) : null}
            </div>,
            document.body,
          )
        : null}
    </div>
  );
};

// 2D floor-plan minimap — aisles × positions, viewport highlights visible columns
interface MinimapProps {
  floorPlan: (BuildingRackHealth | null)[][];
  racksPerAisle: number;
  colsPerPage: number;
  posStart: number;
  onPageSelect: (page: number) => void;
  testId: string;
}

const Minimap = ({ floorPlan, racksPerAisle, colsPerPage, posStart, onPageSelect, testId }: MinimapProps) => {
  const totalPages = Math.ceil(racksPerAisle / colsPerPage);

  return (
    <div className="flex items-stretch gap-1" data-testid={testId}>
      {Array.from({ length: totalPages }, (_, p) => {
        const pStart = p * colsPerPage;
        const pEnd = Math.min(pStart + colsPerPage, racksPerAisle);
        const isCurrent = pStart === posStart;
        return (
          <button
            type="button"
            key={p}
            onClick={() => onPageSelect(p)}
            aria-label={`Page ${p + 1}`}
            className={clsx(
              "grid cursor-pointer gap-0.5 rounded-sm border-2 p-0.5",
              isCurrent ? "border-text-primary" : "border-transparent",
            )}
            style={{
              gridTemplateColumns: `repeat(${colsPerPage}, 4px)`,
              gridTemplateRows: `repeat(${floorPlan.length}, 1fr)`,
            }}
          >
            {floorPlan.flatMap((aisle, ai) =>
              aisle
                .slice(pStart, pEnd)
                .map((rack, ci) => (
                  <div
                    key={`${ai}-${ci}`}
                    className={clsx(
                      "min-h-1 rounded-sm",
                      rack ? MINIMAP_COLORS[worstStatus(rack)] : "bg-core-primary-5",
                    )}
                  />
                )),
            )}
          </button>
        );
      })}
    </div>
  );
};

export default BuildingRackGrid;
