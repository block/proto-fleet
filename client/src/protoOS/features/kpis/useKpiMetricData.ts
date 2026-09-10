import { resolvedKpiViewSelection, useKpiViewSelection } from "@/protoOS/features/kpis/kpiViewSelection";
import { type KpiMetricName, useChartDataForMetric, useMiner } from "@/protoOS/store";

export const useKpiMetricData = (metricName: KpiMetricName) => {
  const selection = resolvedKpiViewSelection(useKpiViewSelection());
  const chart = useChartDataForMetric(metricName, selection);
  const miner = useMiner(selection);

  return { ...chart, miner };
};
