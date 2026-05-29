import {
  formatEfficiency,
  formatHashrate,
  formatLocation,
  formatPowerUsedCapacity,
} from "@/protoFleet/features/sites/utils/formatSiteMetrics";
import Metric from "@/shared/components/Metric";

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
  testId?: string;
}

// Five metrics in the per-site header: Location, Hashrate, Power (used /
// capacity MW), Efficiency, Buildings. The shared Metric primitive
// handles the skeleton vs em-dash vs value rendering so all five tiles
// stay aligned during loading.
const SiteMetricsRow = ({
  locationCity,
  locationState,
  powerCapacityMw,
  buildingCount,
  metrics,
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
      <Metric label="Location" value={location} testId="site-metric-location" />
      <Metric label="Hashrate" value={hashrate} testId="site-metric-hashrate" />
      <Metric label="Power" value={power} testId="site-metric-power" />
      <Metric label="Efficiency" value={efficiency} testId="site-metric-efficiency" />
      <Metric label="Buildings" value={String(buildingCount)} testId="site-metric-buildings" />
    </div>
  );
};

export default SiteMetricsRow;
