import { formatLocation } from "@/protoFleet/features/sites/utils/formatSiteMetrics";
import Metric from "@/shared/components/Metric";
import { formatEfficiency, formatHashrate, formatPowerUsedCapacity } from "@/shared/utils/telemetryFormat";

// Telemetry roll-up shape accepted by SiteMetricsRow. Mirrors the server's
// GetSiteStatsResponse, accepts either it or any subset. Numeric fields are
// 0 when the server didn't compute them (no reporting devices); we still
// surface "—" via formatter helpers. Per-field reporting counts gate the
// display so a missing field (device reporting but field == nil) renders
// "—" instead of a misleading zero / partial average.
interface SiteMetricsRowMetrics {
  totalHashrateThs: number;
  totalPowerKw: number;
  avgEfficiencyJth: number;
  reportingCount: number;
  hashrateReportingCount: number;
  efficiencyReportingCount: number;
  powerReportingCount: number;
}

interface SiteMetricsRowProps {
  locationCity: string;
  locationState: string;
  powerCapacityMw: number;
  buildingCount: number;
  // `undefined` while metrics are still loading; the children render skeletons.
  metrics: SiteMetricsRowMetrics | undefined;
  // Tile size. Defaults to the large `default` Metric scale used on the Sites
  // list; the site detail page passes `compact` for its tighter header strip.
  variant?: "default" | "compact";
  testId?: string;
}

// Five metrics in the per-site header: Location, Buildings, Hashrate, Power
// (used / capacity MW), Efficiency. The shared Metric primitive
// handles the skeleton vs em-dash vs value rendering so all five tiles
// stay aligned during loading.
const SiteMetricsRow = ({
  locationCity,
  locationState,
  powerCapacityMw,
  buildingCount,
  metrics,
  variant = "default",
  testId,
}: SiteMetricsRowProps) => {
  const location = formatLocation(locationCity, locationState);
  // `null` from the formatters → renders as em-dash via Metric.
  const hashrate = metrics
    ? formatHashrate(metrics.hashrateReportingCount > 0 ? metrics.totalHashrateThs : null)
    : undefined;
  const power = metrics
    ? formatPowerUsedCapacity(metrics.powerReportingCount > 0 ? metrics.totalPowerKw : null, powerCapacityMw)
    : undefined;
  const efficiency = metrics
    ? formatEfficiency(metrics.efficiencyReportingCount > 0 ? metrics.avgEfficiencyJth : null)
    : undefined;

  return (
    <div
      className="grid grid-cols-2 gap-6 tablet:grid-cols-3 laptop:grid-cols-5"
      data-testid={testId ?? "site-metrics-row"}
    >
      <Metric label="Location" value={location} variant={variant} testId="site-metric-location" />
      <Metric label="Buildings" value={String(buildingCount)} variant={variant} testId="site-metric-buildings" />
      <Metric label="Hashrate" value={hashrate} variant={variant} testId="site-metric-hashrate" />
      <Metric label="Power" value={power} variant={variant} testId="site-metric-power" />
      <Metric label="Efficiency" value={efficiency} variant={variant} testId="site-metric-efficiency" />
    </div>
  );
};

export default SiteMetricsRow;
