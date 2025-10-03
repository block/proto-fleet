import LineChart, { type LineChartProps } from "@/shared/components/LineChart";

const KpiLineChart = (
  props: Omit<
    LineChartProps,
    "hashboardLocationStore" | "getHashboardColorMap"
  >,
) => {
  return <LineChart {...props} />;
};

export default KpiLineChart;
