import { type MouseEvent as ReactMouseEvent, useCallback, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { createPortal } from "react-dom";

import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { useBuildingStats } from "@/protoFleet/api/useBuildingStats";
import { HealthBar } from "@/protoFleet/components/HealthBar";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { useInViewport } from "@/shared/hooks/useInViewport";

interface BuildingSummaryCardProps {
  building: BuildingWithCounts;
}

const BuildingSummaryCard = ({ building }: BuildingSummaryCardProps) => {
  const id = building.building?.id ?? 0n;
  const idText = id.toString();
  const label = building.building?.name ?? "(unnamed building)";

  const cardRef = useRef<HTMLDivElement | null>(null);
  const isVisible = useInViewport(cardRef);

  const { stats, error: statsError } = useBuildingStats({
    buildingId: id,
    enabled: id !== 0n && isVisible,
    pollIntervalMs: POLL_INTERVAL_MS,
  });

  const navigate = useNavigate();
  const goToDetail = () => navigate(`/buildings/${idText}`);

  const hasStats = stats !== undefined;

  const [hoverPos, setHoverPos] = useState<{ x: number; y: number } | null>(null);
  const popoverRef = useRef<HTMLDivElement>(null);

  const handleMouseMove = useCallback(
    (e: ReactMouseEvent) => {
      if (!hasStats) return;
      setHoverPos({ x: e.clientX, y: e.clientY });
    },
    [hasStats],
  );

  const handleMouseLeave = useCallback(() => setHoverPos(null), []);

  const popoverStyle = useMemo(() => {
    if (!hoverPos) return undefined;
    const popoverH = popoverRef.current?.offsetHeight ?? 60;
    return {
      left: hoverPos.x + 12,
      top: hoverPos.y - popoverH - 12,
    };
  }, [hoverPos]);

  return (
    <>
      <div
        ref={cardRef}
        role="link"
        tabIndex={0}
        onClick={goToDetail}
        onKeyDown={(e) => {
          if (e.key !== "Enter" && e.key !== " ") return;
          e.preventDefault();
          goToDetail();
        }}
        onMouseMove={handleMouseMove}
        onMouseLeave={handleMouseLeave}
        className="flex h-full cursor-pointer flex-col items-center justify-center gap-3 rounded-2xl bg-surface-overlay px-5 py-6 transition-opacity hover:opacity-80"
        data-testid={`building-card-${idText}`}
      >
        <span
          className="max-w-full min-w-0 truncate text-emphasis-300 text-text-primary"
          data-testid={`building-card-${idText}-name`}
        >
          {label}
        </span>

        <div className="w-full">
          {stats === undefined && statsError ? (
            <span
              className="block text-center text-200 text-intent-critical-text"
              data-testid={`building-card-${idText}-stats-error`}
            >
              Couldn&apos;t load stats
            </span>
          ) : stats === undefined ? (
            <SkeletonBar className="h-1.5 w-full" />
          ) : (
            <HealthBar
              healthy={stats.hashingCount}
              needsAttention={stats.brokenCount}
              offline={stats.offlineCount}
              sleeping={stats.sleepingCount}
              testId={`building-card-${idText}-health`}
            />
          )}
        </div>
      </div>

      {hoverPos && hasStats
        ? createPortal(
            <div
              ref={popoverRef}
              className="pointer-events-none fixed z-50 rounded-2xl border border-border-5 bg-surface-elevated-base px-5 py-4 shadow-300"
              style={popoverStyle}
              data-testid={`building-card-${idText}-popover`}
            >
              <div className="mb-2 text-emphasis-300 text-text-primary">{label}</div>
              {stats!.hashingCount > 0 ? (
                <div className="flex items-center gap-2 text-300 text-text-primary">
                  <span className="inline-block size-2 shrink-0 rounded-full bg-text-primary" />
                  {stats!.hashingCount} healthy
                </div>
              ) : null}
              {stats!.brokenCount > 0 ? (
                <div className="mt-1.5 flex items-center gap-2 text-300 text-text-primary">
                  <span className="inline-block size-2 shrink-0 rounded-full bg-intent-critical-fill" />
                  {stats!.brokenCount} need attention
                </div>
              ) : null}
              {stats!.offlineCount > 0 ? (
                <div className="mt-1.5 flex items-center gap-2 text-300 text-text-primary">
                  <span className="inline-block size-2 shrink-0 rounded-full bg-intent-warning-fill" />
                  {stats!.offlineCount} offline
                </div>
              ) : null}
              {stats!.sleepingCount > 0 ? (
                <div className="mt-1.5 flex items-center gap-2 text-300 text-text-primary">
                  <span className="inline-block size-2 shrink-0 rounded-full bg-core-primary-20" />
                  {stats!.sleepingCount} sleeping
                </div>
              ) : null}
            </div>,
            document.body,
          )
        : null}
    </>
  );
};

export default BuildingSummaryCard;
