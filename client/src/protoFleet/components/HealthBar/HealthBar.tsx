import { useMemo } from "react";
import clsx from "clsx";

export interface HealthBarProps {
  healthy: number;
  needsAttention: number;
  offline: number;
  sleeping: number;
  empty?: number;
  className?: string;
  testId?: string;
}

interface SegmentDef {
  key: string;
  count: number;
  colorClass: string;
  heightClass: string;
}

const MIN_PERCENTAGE = 1;

const HealthBar = ({ healthy, needsAttention, offline, sleeping, empty = 0, className, testId }: HealthBarProps) => {
  const segments = useMemo<SegmentDef[]>(() => {
    const raw: SegmentDef[] = [
      { key: "healthy", count: healthy, colorClass: "bg-text-primary", heightClass: "h-[2px]" },
      { key: "needsAttention", count: needsAttention, colorClass: "bg-intent-critical-fill", heightClass: "h-full" },
      { key: "offline", count: offline, colorClass: "bg-intent-warning-fill", heightClass: "h-full" },
      { key: "sleeping", count: sleeping, colorClass: "bg-core-primary-20", heightClass: "h-full" },
      { key: "empty", count: empty, colorClass: "bg-transparent", heightClass: "h-full" },
    ];
    return raw.filter((s) => s.count > 0);
  }, [healthy, needsAttention, offline, sleeping, empty]);

  const total = useMemo(() => segments.reduce((sum, s) => sum + s.count, 0), [segments]);

  if (total === 0) {
    return (
      <div
        className={clsx("h-1.5 w-full rounded-[3px] bg-core-primary-10", className)}
        data-testid={testId}
        role="img"
        aria-label="No data"
      />
    );
  }

  return (
    <div
      className={clsx("flex h-1.5 w-full items-center gap-[1px]", className)}
      data-testid={testId}
      role="img"
      aria-label={`Health: ${healthy} healthy, ${needsAttention} need attention, ${offline} offline, ${sleeping} sleeping`}
    >
      {segments.map((seg) => {
        const pct = Math.max((seg.count / total) * 100, MIN_PERCENTAGE);
        return (
          <div
            key={seg.key}
            className={clsx(seg.colorClass, seg.heightClass, "rounded-[3px]")}
            style={{ width: `${pct}%` }}
          />
        );
      })}
    </div>
  );
};

export default HealthBar;
