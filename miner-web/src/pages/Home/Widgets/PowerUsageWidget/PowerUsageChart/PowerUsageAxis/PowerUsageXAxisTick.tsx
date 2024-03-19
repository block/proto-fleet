import PowerUsageAxisTick from "./PowerUsageAxisTick";

interface PowerUsageXAxisTickProps {
  payload: { value: string; index: number };
  x: number;
  y: number;
}

const PowerUsageXAxisTick = ({ x, y, payload }: PowerUsageXAxisTickProps) => {
  const { index } = payload;
  if (index === 0 || index === 12 || index === 23) {
    return (
      <PowerUsageAxisTick
        x={x}
        y={y}
        xOffset={8}
        value={index === 23 ? "Now" : payload.value}
      />
    );
  }

  return <></>;
};

export default PowerUsageXAxisTick;
