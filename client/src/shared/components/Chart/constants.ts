import AxisTick from "./AxisTick";

export const xAxisProps = {
  dataKey: "datetime",
  axisLine: false,
  tickLine: false,
  interval: 0,
  tickMargin: 18,
};

export const yAxisProps = {
  axisLine: false,
  tickLine: false,
  tick: AxisTick,
  tickSize: 0,
  interval: 0,
  tickMargin: 0,
};
