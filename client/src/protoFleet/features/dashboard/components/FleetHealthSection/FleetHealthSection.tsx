import { Link } from "react-router-dom";
import SiteResourcePanel from "./SiteResourcePanel";
import { useSiteStats } from "@/protoFleet/api/useSiteStats";
import { HealthBar } from "@/protoFleet/components/HealthBar";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import FleetHealthMetrics from "@/protoFleet/features/dashboard/components/FleetHealthMetrics";
import SectionHeading from "@/protoFleet/features/dashboard/components/SectionHeading";
import { scopedPath } from "@/protoFleet/routing/siteScope";
import { type ActiveSite } from "@/protoFleet/store/types/activeSite";
import Button, { sizes, variants } from "@/shared/components/Button";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { formatEfficiency, formatHashrate, KW_PER_MW } from "@/shared/utils/telemetryFormat";

type MinerCount = number | null | undefined;

interface FleetHealthSectionProps {
  activeSite: ActiveSite;
  /** The selected site's id, for the GetSiteStats roll-up behind the subheading. */
  siteId: bigint;
  /** Site power capacity (MW) for the "N% of Y MW" portion of the subheading. */
  powerCapacityMw: number;
  fleetSize?: MinerCount;
  healthyMiners?: MinerCount;
  needsAttentionMiners?: MinerCount;
  offlineMiners?: MinerCount;
  sleepingMiners?: MinerCount;
}

// Drop the trailing ".0" so a 12 MW capacity reads "12 MW", not "12.0 MW",
// while a 12.5 MW capacity keeps its decimal.
const formatMw = (mw: number) => (Number.isInteger(mw) ? String(mw) : mw.toFixed(1));

const LegendDot = ({ colorClass, label }: { colorClass: string; label: string }) => (
  <div className="flex items-center gap-2">
    <span className={`h-3 w-3 rounded-full ${colorClass}`} />
    <span className="text-grayscale-gray-70">{label}</span>
  </div>
);

// Single-site "Fleet health" section: a header with the live performance
// summary and quick links, then a module card holding the health metric
// tiles, the HealthBar, its legend, and an FPO slot for building / rack /
// component status to come.
const FleetHealthSection = ({
  activeSite,
  siteId,
  powerCapacityMw,
  fleetSize,
  healthyMiners,
  needsAttentionMiners,
  offlineMiners,
  sleepingMiners,
}: FleetHealthSectionProps) => {
  const { stats } = useSiteStats({ siteId, enabled: siteId !== 0n, pollIntervalMs: POLL_INTERVAL_MS });

  // Build the performance subheading from the site roll-up, gating each
  // metric on its own reporting count so a non-reporting field is dropped
  // rather than shown as a misleading zero.
  const subheadingParts: string[] = [];
  if (stats) {
    const hashrate = stats.hashrateReportingCount > 0 ? formatHashrate(stats.totalHashrateThs) : null;
    if (hashrate) subheadingParts.push(hashrate);

    if (stats.powerReportingCount > 0) {
      const usedMw = stats.totalPowerKw / KW_PER_MW;
      subheadingParts.push(
        powerCapacityMw > 0
          ? `${usedMw.toFixed(1)} MW (${Math.round((usedMw / powerCapacityMw) * 100)}% of ${formatMw(powerCapacityMw)} MW)`
          : `${usedMw.toFixed(1)} MW`,
      );
    }

    const efficiency = stats.efficiencyReportingCount > 0 ? formatEfficiency(stats.avgEfficiencyJth) : null;
    if (efficiency) subheadingParts.push(efficiency);
  }

  // HealthBar takes raw counts; only render it once every bucket is a real
  // number (loading → undefined, no-data → null both fall back to a skeleton).
  const barReady =
    typeof healthyMiners === "number" &&
    typeof needsAttentionMiners === "number" &&
    typeof offlineMiners === "number" &&
    typeof sleepingMiners === "number";

  return (
    <section data-testid="dashboard-fleet-health-section">
      <SectionHeading heading="Fleet health">
        <div className="flex items-center gap-2">
          <Link to={scopedPath("/fleet/sites", activeSite)} data-testid="dashboard-fleet-health-view-sites">
            <Button variant={variants.secondary} size={sizes.compact} text="View sites" />
          </Link>
          <Link to={scopedPath("/fleet/miners", activeSite)} data-testid="dashboard-fleet-health-view-miners">
            <Button variant={variants.secondary} size={sizes.compact} text="View miners" />
          </Link>
        </div>
      </SectionHeading>

      {stats === undefined ? (
        <SkeletonBar className="mt-2 h-4 w-80 max-w-full" />
      ) : subheadingParts.length > 0 ? (
        <p className="mt-2 text-300 text-text-primary-70" data-testid="dashboard-fleet-health-subheading">
          {subheadingParts.join(", ")}
        </p>
      ) : null}

      <div className="mt-6 rounded-xl bg-surface-base p-10 dark:bg-core-primary-5 phone:p-6">
        <FleetHealthMetrics
          fleetSize={fleetSize}
          healthyMiners={healthyMiners}
          needsAttentionMiners={needsAttentionMiners}
          offlineMiners={offlineMiners}
          sleepingMiners={sleepingMiners}
        />

        <div className="mt-10 w-full">
          {barReady ? (
            <HealthBar
              healthy={healthyMiners}
              needsAttention={needsAttentionMiners}
              offline={offlineMiners}
              sleeping={sleepingMiners}
              testId="dashboard-fleet-health-bar"
            />
          ) : (
            <SkeletonBar className="h-1.5 w-full" />
          )}
        </div>

        <div className="mt-4 flex flex-wrap items-center gap-6 text-sm">
          <LegendDot colorClass="bg-text-primary" label="Healthy" />
          <LegendDot colorClass="bg-intent-critical-fill" label="Needs attention" />
          <LegendDot colorClass="bg-intent-warning-fill" label="Offline" />
          <LegendDot colorClass="bg-core-primary-20" label="Sleeping" />
        </div>

        <SiteResourcePanel siteId={siteId} activeSite={activeSite} />
      </div>
    </section>
  );
};

export default FleetHealthSection;
