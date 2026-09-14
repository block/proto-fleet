import type { TimeSeriesLevel } from "@/protoOS/api/adapters/types";
import type { KpiMetricName } from "@/protoOS/store";

export type KpiTimeSeriesField = Extract<TimeSeriesLevel, { type: "miner" }>["fields"][number];

// The all-metrics set the miner-level aggregate series fetches for the tabnav
// summary. Every KPI tab reads from it, so it carries all four fields.
export const kpiFields = (): KpiTimeSeriesField[] => ["hashrate", "power", "efficiency", "temperature"];

// Only the active tab's metric is charted, so the per-board fan-out fetches
// just that metric instead of all four (a 3-4x cut on container Home, where the
// per-board series is one request per board per field). Efficiency requests the
// "efficiency" field itself: the per-board chart segments read telemetry.efficiency
// directly, which the rig adapter serves natively and the CT1 adapter expands to
// hashrate+power and derives. Requesting raw hashrate+power here would leave
// telemetry.efficiency unpopulated on the rig and blank the per-board series.
export const perBoardFieldsForMetric = (metric: KpiMetricName): KpiTimeSeriesField[] => {
  switch (metric) {
    case "power":
      return ["power"];
    case "temperature":
      return ["temperature"];
    case "efficiency":
      return ["efficiency"];
    case "hashrate":
      return ["hashrate"];
    default:
      // Exhaustiveness guard: a new KpiMetricName must add a case above.
      metric satisfies never;
      return ["hashrate"];
  }
};

// Maps a KPI tab's route segment (the `:metric` param) to its metric name.
// Home's index route carries no segment, so `undefined` (and any unknown value)
// falls back to hashrate. Deriving from the matched route param instead of
// scanning the full pathname avoids matching a metric word that appears earlier
// in the path (e.g. a miner whose id is "temperature" sitting at Home root).
export const metricFromRouteParam = (param: string | undefined): KpiMetricName => {
  switch (param) {
    case "power-usage":
      return "power";
    case "efficiency":
      return "efficiency";
    case "temperature":
      return "temperature";
    case "hashrate":
      return "hashrate";
    default:
      return "hashrate";
  }
};
