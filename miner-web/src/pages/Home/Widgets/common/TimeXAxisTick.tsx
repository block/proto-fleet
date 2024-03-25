import AxisTick from "./AxisTick";

interface TimeXAxisTickProps {
  payload: { value: string; index: number };
  x: number;
  y: number;
}

const TimeXAxisTick = ({ x, y, payload }: TimeXAxisTickProps) => {
  const { index } = payload;
  if (index === 0 || index === 12 || index === 23) {
    return (
      <AxisTick
        x={x}
        y={y}
        xOffset={index === 23 ? 8 : 16}
        payload={{ ...payload, value: index === 23 ? "Now" : payload.value }}
      />
    );
  }

  return <></>;
};

export default TimeXAxisTick;
