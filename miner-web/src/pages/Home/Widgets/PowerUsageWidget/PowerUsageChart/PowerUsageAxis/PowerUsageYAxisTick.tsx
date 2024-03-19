import PowerUsageAxisTick from "./PowerUsageAxisTick";

interface PowerUsageYAxisTickProps {
  payload: { value: string; index: number };
  x: number;
  y: number;
}

const PowerUsageYAxisTick = ({ x, y, payload }: PowerUsageYAxisTickProps) => {
  return <PowerUsageAxisTick x={x} y={y} value={payload.value} />;
};

export default PowerUsageYAxisTick;
