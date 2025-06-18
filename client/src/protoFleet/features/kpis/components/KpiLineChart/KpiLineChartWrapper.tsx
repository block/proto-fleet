import KpiChart, {
  type KpiChartProps,
} from "@/shared/features/kpis/components/KpiLineChart";

const KpiLineChartWrapper = (
  props: Omit<KpiChartProps, "hashboardLocationStore" | "getHashboardColorMap">,
) => {
  return <KpiChart {...props} />;
};

export default KpiLineChartWrapper;
