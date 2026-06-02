import { type GetBuildingStatsResponse } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import Metric from "@/shared/components/Metric";
import { separateByCommas } from "@/shared/utils/stringUtils";
import { formatEfficiency, formatHashrate, formatPowerUsedCapacity, KW_PER_MW } from "@/shared/utils/telemetryFormat";

interface BuildingMetricsRowProps {
  // Building capacity (power_kw on the Building proto). 0 = unset → renders
  // as an em dash on the capacity side of the power tile.
  powerCapacityKw: number;
  // `undefined` while stats are still loading; the Metric primitive renders
  // skeletons. `null` on any cell is the no-data em dash.
  stats: GetBuildingStatsResponse | undefined;
  testId?: string;
}

const formatMinersOnline = (hashing: number, total: number): string => {
  if (total === 0) return "0 / 0";
  return `${separateByCommas(hashing)} / ${separateByCommas(total)}`;
};

// Four-metric header for /buildings/:id. Mirrors the rack-overview top
// strip (hashrate / power / efficiency / online) but power adds a capacity
// denominator from the building's power_kw config field.
const BuildingMetricsRow = ({ powerCapacityKw, stats, testId }: BuildingMetricsRowProps) => {
  // Per-field reporting counts so a device that reported state but not
  // efficiency doesn't pull the building average toward a misleading
  // partial number — render the dash instead.
  const hashrate = stats ? formatHashrate(stats.hashrateReportingCount > 0 ? stats.totalHashrateThs : null) : undefined;
  // Shared formatter expects capacity in MW; the Building proto stores
  // power_kw, so convert at the call site.
  const power = stats
    ? formatPowerUsedCapacity(stats.powerReportingCount > 0 ? stats.totalPowerKw : null, powerCapacityKw / KW_PER_MW)
    : undefined;
  const efficiency = stats
    ? formatEfficiency(stats.efficiencyReportingCount > 0 ? stats.avgEfficiencyJth : null)
    : undefined;
  const onlineDisplay = stats ? formatMinersOnline(stats.hashingCount, stats.deviceCount) : undefined;

  return (
    <div className="grid grid-cols-2 gap-6 tablet:grid-cols-4" data-testid={testId ?? "building-metrics-row"}>
      <Metric label="Hashrate" value={hashrate} testId="building-metric-hashrate" />
      <Metric label="Power" value={power} testId="building-metric-power" />
      <Metric label="Efficiency" value={efficiency} testId="building-metric-efficiency" />
      <Metric label="Miners online" value={onlineDisplay} testId="building-metric-online" />
    </div>
  );
};

export default BuildingMetricsRow;
