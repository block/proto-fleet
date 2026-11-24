import { type ReactNode } from "react";
import clsx from "clsx";
import { formatAcceptanceRate, formatShareCount, getAcceptanceRate, normalizeShareCount } from "./poolStats";
import { Circle, Triangle } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import CompositionBar from "@/shared/components/CompositionBar";
import StatusCircle from "@/shared/components/StatusCircle";

/** A pool's role in the priority chain: the one currently hashing vs a backup. */
export type PoolRole = "active" | "standby";

export interface ContainerPool {
  id: string;
  /** Display name, e.g. "Default pool", "Backup 1". */
  name: string;
  /** Pool host/URL, shown as a subtitle. */
  url?: string;
  /** Position in the priority chain. */
  role: PoolRole;
  /** Shares accepted by the pool as valid solutions. */
  accepted: number;
  /** Shares the pool rejected (e.g. too low difficulty). */
  rejected: number;
  /** Shares dropped locally before reaching the pool. */
  invalid: number;
  /** Pre-formatted current pool difficulty, e.g. "524.3K". */
  difficulty: string;
  /** Pre-formatted last-share time, e.g. "2s ago". */
  lastShare: string;
  /** Pre-formatted best-share difficulty, e.g. "165.6B". */
  bestShare: string;
  /** Count of blocks seen while mining this pool. */
  blocks: number;
}

interface PoolMonitorCardProps extends ContainerPool {
  /**
   * When true, this pool gets the design's prominent treatment: the share
   * stats + composition bar + legend are wrapped in an elevated panel. Backups
   * render as a compact pair of flat stat rows.
   */
  prominent?: boolean;
}

/** Design's black / red / grey split, overriding CompositionBar's semantic defaults. */
const BAR_COLOR_MAP = {
  OK: "bg-core-primary-fill",
  WARNING: "bg-intent-critical-fill",
  NA: "bg-core-primary-20",
} as const;

/** A read-only label-over-value stat cell matching the design's type scale. */
const PoolStat = ({ label, value, large = false }: { label: string; value: string; large?: boolean }) => (
  <div className="flex min-w-0 flex-col gap-1" data-testid="pool-stat">
    <div className="text-heading-50 text-text-primary-50">{label}</div>
    <div className={clsx("truncate text-text-primary", large ? "text-heading-300" : "text-heading-100")}>{value}</div>
  </div>
);

const LegendItem = ({ icon, label }: { icon: ReactNode; label: string }) => (
  <div className="flex items-center gap-1.5 text-200 text-text-primary-70">
    {icon}
    <span>{label}</span>
  </div>
);

/**
 * A single pool in the container's read-only Pools monitoring view (Frame 3).
 * Distinct from the Settings → Mining Pools config form: this shows live
 * share/difficulty telemetry, not editable connection settings. The default
 * (active) pool gets a prominent treatment — its share stats, an
 * accepted/rejected/invalid split bar, and a legend are grouped in an elevated
 * panel — while backups render compactly as two flat stat rows. Acceptance rate
 * is derived from the three share counts so the headline always agrees with the
 * bar; counts abbreviate the way the design shows them (71.9K, 165.6B).
 */
const PoolMonitorCard = ({
  name,
  url,
  role,
  accepted,
  rejected,
  invalid,
  difficulty,
  lastShare,
  bestShare,
  blocks,
  prominent = false,
}: PoolMonitorCardProps) => {
  const isActive = role === "active";
  const normalizedAccepted = normalizeShareCount(accepted);
  const normalizedRejected = normalizeShareCount(rejected);
  const normalizedInvalid = normalizeShareCount(invalid);
  const acceptanceLabel = formatAcceptanceRate(
    getAcceptanceRate(normalizedAccepted, normalizedRejected, normalizedInvalid),
  );

  const shareStats = (large: boolean) => (
    <div className="grid grid-cols-4 gap-x-6 gap-y-4 phone:grid-cols-2">
      <PoolStat label="Acceptance rate" value={acceptanceLabel} large={large} />
      <PoolStat label="Accepted" value={formatShareCount(normalizedAccepted)} large={large} />
      <PoolStat label="Rejected" value={formatShareCount(normalizedRejected)} large={large} />
      <PoolStat label="Invalid" value={formatShareCount(normalizedInvalid)} large={large} />
    </div>
  );

  const detailStats = (
    <div className="grid grid-cols-4 gap-x-6 gap-y-4 phone:grid-cols-2">
      <PoolStat label="Difficulty" value={difficulty} />
      <PoolStat label="Last share" value={lastShare} />
      <PoolStat label="Best share" value={bestShare} />
      <PoolStat label="Blocks" value={formatShareCount(blocks)} />
    </div>
  );

  return (
    <div className="flex min-w-0 flex-col gap-6" data-testid="pool-monitor-card" data-prominent={prominent}>
      {/* Header: name + host (left), role status (right) */}
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <span className="truncate text-heading-300 text-text-primary" data-testid="pool-monitor-name">
            {name}
          </span>
          {url ? <span className="truncate text-300 text-text-primary-70">{url}</span> : null}
        </div>
        <div className="flex shrink-0 items-center gap-2" data-testid="pool-monitor-status">
          <StatusCircle
            status={isActive ? "normal" : "inactive"}
            variant="simple"
            width={iconSizes.xSmall}
            removeMargin
          />
          <span className="text-300 text-text-primary">{isActive ? "Active" : "Standby"}</span>
        </div>
      </div>

      {prominent ? (
        <>
          {/* Prominent: share stats + split bar + legend in an elevated panel */}
          <div className="flex flex-col gap-5 rounded-2xl bg-surface-overlay p-6" data-testid="pool-monitor-panel">
            {shareStats(true)}
            <CompositionBar
              height={10}
              colorMap={BAR_COLOR_MAP}
              segments={[
                { name: "Accepted", status: "OK", count: normalizedAccepted },
                { name: "Rejected", status: "WARNING", count: normalizedRejected },
                { name: "Invalid", status: "NA", count: normalizedInvalid },
              ]}
            />
            <div className="flex flex-wrap items-center gap-6">
              <LegendItem
                icon={<Circle className="text-core-primary-fill" width={iconSizes.xSmall} />}
                label="Accepted"
              />
              <LegendItem icon={<Triangle className="h-3 w-3 text-intent-critical-fill" />} label="Rejected" />
              <LegendItem icon={<Circle className="text-core-primary-20" width={iconSizes.xSmall} />} label="Invalid" />
            </div>
          </div>
          {detailStats}
        </>
      ) : (
        /* Compact: two flat stat rows, no panel/bar/legend */
        <div className="flex flex-col gap-6">
          {shareStats(false)}
          {detailStats}
        </div>
      )}
    </div>
  );
};

export default PoolMonitorCard;
export type { PoolMonitorCardProps };
