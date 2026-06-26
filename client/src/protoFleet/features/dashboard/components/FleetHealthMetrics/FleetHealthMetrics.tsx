import clsx from "clsx";
import Stats from "@/shared/components/Stats";

// undefined = still loading (skeleton), null = loaded but no data (em-dash),
// number = render the value. Mirrors the convention used by FleetHealth.
type MinerCount = number | null | undefined;

export interface FleetHealthMetricsProps {
  fleetSize?: MinerCount;
  healthyMiners?: MinerCount;
  needsAttentionMiners?: MinerCount;
  offlineMiners?: MinerCount;
  sleepingMiners?: MinerCount;
  className?: string;
  /** Grid override so callers can tune the column layout per surface. */
  grid?: string;
}

const EMDASH = "—";
const minersLabel = (count: number) => (count === 1 ? "miner" : "miners");

// The flat fleet-health metric tiles shared by both dashboard modes:
// All Sites renders them bare; a single site wraps them in the Fleet
// health module above the HealthBar. No card chrome, no composition bar —
// just the numbers.
const FleetHealthMetrics = ({
  fleetSize,
  healthyMiners,
  needsAttentionMiners,
  offlineMiners,
  sleepingMiners,
  className,
  grid = "grid-cols-5 phone:grid-cols-2 phone:gap-y-6",
}: FleetHealthMetricsProps) => {
  const counts = [fleetSize, healthyMiners, needsAttentionMiners, offlineMiners, sleepingMiners];
  const isLoading = counts.some((c) => c === undefined);

  // Total is the denominator for the per-status percentages. Guard against
  // division by zero when a fleet has no miners (every status is 0 → 0%).
  const total = typeof fleetSize === "number" && fleetSize > 0 ? fleetSize : 1;

  const statusStat = (label: string, count: MinerCount) => {
    if (isLoading) return { label, value: undefined };
    if (count === null || count === undefined) return { label, value: EMDASH };
    const pct = Math.round((count / total) * 100);
    return { label, value: `${pct}%`, text: `${count} ${minersLabel(count)}` };
  };

  const fleetStat = isLoading
    ? { label: "Your fleet", value: undefined }
    : fleetSize === null || fleetSize === undefined
      ? { label: "Your fleet", value: EMDASH }
      : { label: "Your fleet", value: `${fleetSize} ${minersLabel(fleetSize)}` };

  return (
    <div className={clsx("w-full", className)}>
      <Stats
        stats={[
          fleetStat,
          statusStat("Health", healthyMiners),
          statusStat("Needs attention", needsAttentionMiners),
          statusStat("Offline", offlineMiners),
          statusStat("Sleeping", sleepingMiners),
        ]}
        size="large"
        grid={grid}
        gap="gap-x-10 phone:gap-6"
        padding=""
      />
    </div>
  );
};

export default FleetHealthMetrics;
