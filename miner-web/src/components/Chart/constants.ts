import AxisTick from "./AxisTick";
import TimeXAxisTick from "./TimeXAxisTick";

export const xAxisProps = {
  dataKey: "time",
  axisLine: false,
  tickLine: false,
  interval: 0,
  tick: TimeXAxisTick,
};

export const yAxisProps = {
  axisLine: false,
  tickLine: false,
  tick: AxisTick,
  interval: 0,
  tickMargin: 12,
};
