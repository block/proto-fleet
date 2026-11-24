import clsx from "clsx";
import { type PoolDuration, poolDurations } from "./usePoolStatistics";
import type { PoolHistoryResult, PoolShareStatistics } from "@/protoOS/api/adapters/poolHistory";
import { isActivePool } from "@/protoOS/api/adapters/poolStatistics";
import type { PoolInfo } from "@/protoOS/store/contract";
import { Circle, Triangle } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import DurationSelector from "@/shared/components/DurationSelector";
import Stat from "@/shared/components/Stat";

const compact = new Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 });
const format = (value: number | undefined) => (value === undefined ? "—" : compact.format(value));
const formatAssignedDifficulty = (value: string | undefined) => {
  if (value === undefined) return "—";
  try {
    return compact.format(BigInt(value));
  } catch {
    return "—";
  }
};
const formatResultTime = (value: number | undefined) =>
  value === undefined || !Number.isFinite(value)
    ? "—"
    : new Date(value).toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" });
const resultOutcome = {
  accepted: "Accepted",
  rejected: "Rejected",
  submitError: "Submission failed",
  unknown: "Outcome unavailable",
} as const;

interface Props {
  usernameLabel?: "Worker" | "Username";
  pools?: PoolInfo[];
  history?: PoolHistoryResult;
  duration: PoolDuration;
  onDurationChange: (duration: PoolDuration) => void;
  configureHref: string;
  pending?: boolean;
  error?: string;
}

function ShareBar({ statistics }: { statistics?: PoolShareStatistics }) {
  const rate = statistics?.acceptanceRate;
  return (
    <div className="mt-6">
      <div
        role="img"
        aria-label={
          rate === undefined
            ? "Share proportions unavailable"
            : `${(rate * 100).toFixed(1)}% accepted, ${((1 - rate) * 100).toFixed(1)}% rejected`
        }
        className="flex h-3 gap-2"
      >
        {rate === undefined ? (
          <div className="h-full w-full rounded-full bg-core-primary-10" />
        ) : (
          <>
            {rate > 0 ? <div className="h-full rounded-full bg-text-primary" style={{ flex: rate }} /> : null}
            {rate < 1 ? (
              <div className="h-full rounded-full bg-intent-critical-fill" style={{ flex: 1 - rate }} />
            ) : null}
          </>
        )}
      </div>
      <div className="mt-3 flex gap-4 text-200">
        <span className="flex items-center gap-2">
          <span aria-hidden="true">
            <Circle width="w-3" className="text-text-primary" />
          </span>
          Accepted
        </span>
        <span className="flex items-center gap-2">
          <span aria-hidden="true">
            <Triangle className="text-intent-critical-fill" />
          </span>
          Rejected
        </span>
      </div>
    </div>
  );
}

function PoolSection({
  pool,
  index,
  statistics,
  unavailable,
  usernameLabel,
}: {
  pool: PoolInfo;
  usernameLabel: "Worker" | "Username";
  index: number;
  statistics?: PoolShareStatistics;
  unavailable: boolean;
}) {
  const active = isActivePool(pool);
  const title = pool.name || (index === 0 ? "Default pool" : `Backup ${index}`);
  const status = pool.status === "Idle" ? "Standby" : (pool.status ?? "Unknown");
  return (
    <section
      aria-label={title}
      className={clsx(
        "space-y-6 border-b border-border-10 p-4 last:border-b-0 tablet:p-10",
        index === 0 && "pt-6 tablet:pt-6",
      )}
    >
      <div>
        <h2 className="text-heading-300">{title}</h2>
        <div className="flex items-start justify-between gap-4">
          <p className="min-w-0 text-400 break-all">{pool.url || "—"}</p>
          <span className={clsx("flex shrink-0 items-center gap-1 text-400", !active && "text-text-primary-50")}>
            <span aria-hidden="true">
              <Circle
                width="w-2"
                className={
                  active
                    ? "text-intent-success-fill"
                    : pool.status === "Dead"
                      ? "text-intent-critical-fill"
                      : "text-core-primary-20"
                }
              />
            </span>
            {status}
          </span>
        </div>
        <p className="mt-2 text-300 break-all text-text-primary-50">
          {usernameLabel}: {pool.username || "—"}
        </p>
      </div>
      <div className={clsx(active && "rounded-xl bg-core-primary-5 p-4 tablet:p-10")}>
        <div className="grid grid-cols-2 gap-x-4 gap-y-6 desktop:grid-cols-4">
          <Stat
            preformatted
            label="Acceptance rate"
            value={statistics?.acceptanceRate === undefined ? "—" : `${(statistics.acceptanceRate * 100).toFixed(1)}%`}
            size={active ? "large" : "small"}
          />
          <Stat preformatted label="Accepted" value={format(statistics?.accepted)} size={active ? "large" : "small"} />
          <Stat preformatted label="Rejected" value={format(statistics?.rejected)} size={active ? "large" : "small"} />
          <Stat
            preformatted
            label="Last result time"
            value={formatResultTime(pool.lastShareAtMs)}
            text={pool.lastShareOutcome ? resultOutcome[pool.lastShareOutcome] : undefined}
            size={active ? "large" : "small"}
          />
        </div>
        {active ? <ShareBar statistics={statistics} /> : null}
      </div>
      <div className="grid grid-cols-2 gap-x-4 gap-y-6 desktop:grid-cols-4">
        <Stat preformatted label="Difficulty" value={format(pool.difficulty)} size="small" />
        <Stat
          preformatted
          label="Last assigned difficulty"
          value={formatAssignedDifficulty(pool.lastAssignedDifficulty)}
          size="small"
        />
        <Stat preformatted label="Best share" value="—" size="small" />
        <Stat preformatted label="Blocks" value="—" size="small" />
      </div>
      {unavailable ? (
        <p className="text-200 text-text-primary-50">Share history unavailable.</p>
      ) : statistics?.partial ? (
        <p className="text-200 text-text-primary-50">Partial share history for this period.</p>
      ) : null}
    </section>
  );
}

export default function PoolStatisticsView({
  pools,
  usernameLabel = "Worker",
  history,
  duration,
  onDurationChange,
  configureHref,
  pending,
  error,
}: Props) {
  return (
    <div className="min-w-0 pb-6">
      <h1 className="sr-only">Pools</h1>
      <div className="flex flex-wrap items-center justify-between gap-4 px-4 pt-4 tablet:px-10 tablet:pt-10">
        <DurationSelector duration={duration} durations={poolDurations} onSelect={onDurationChange} />
        <Button text="Configure pools" variant="secondary" size="compact" to={configureHref} />
      </div>
      {error ? (
        <p role="alert" className="px-4 pt-6 text-300 text-text-critical tablet:px-10">
          {error}
        </p>
      ) : null}
      {pending && !history ? (
        <p role="status" className="px-4 pt-6 text-200 text-text-primary-50 tablet:px-10">
          Loading pool statistics…
        </p>
      ) : null}
      {pools?.length === 0 ? (
        <p className="p-4 text-400 tablet:p-10">No pools configured. Configure a pool to start mining.</p>
      ) : null}
      {pools?.map((pool, index) => (
        <PoolSection
          key={pool.id}
          pool={pool}
          usernameLabel={usernameLabel}
          index={index}
          statistics={history?.byPool[pool.id]}
          unavailable={history?.unavailable.includes(pool.id) ?? false}
        />
      ))}
    </div>
  );
}
