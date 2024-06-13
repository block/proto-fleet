import AxisTick from "./AxisTick";

export const xAxisProps = {
  dataKey: "time",
  axisLine: false,
  tickLine: false,
  interval: 0,
  tickMargin: 18,
};

export const yAxisProps = {
  axisLine: false,
  tickLine: false,
  tick: AxisTick,
  interval: 0,
  tickMargin: 12,
};
