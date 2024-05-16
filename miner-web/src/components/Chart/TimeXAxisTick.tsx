import { AxisTick } from "components/Chart";

interface TimeXAxisTickProps {
  payload: { value: string; index: number; offset: number };
  visibleTicksCount: number;
  x: number;
  y: number;
}

const TimeXAxisTick = ({ x, y, payload, visibleTicksCount }: TimeXAxisTickProps) => {
  const { index } = payload;
  const firstTick = index === 0;
  const lastTick = index === visibleTicksCount - 1;
  // show time for every 6th tick but maintain more than two tick gap before last tick
  const midTick = index % 6 === 0 && index < visibleTicksCount - 2;
  if (firstTick || midTick || lastTick) {
    let xOffset = 0;
    if (firstTick) {
      // the offset needed to add margin left to the first tick
      xOffset = 25 - payload.offset;
    } else if (midTick) {
      // the offset needed to center the mid ticks
      xOffset = 16 + payload.offset;
    } else if (lastTick) {
      // the offset needed to add margin right to the first tick
      xOffset = 0 + payload.offset;
    }
    return (
      <AxisTick
        x={x}
        y={y}
        xOffset={xOffset}
        payload={{ ...payload, value: payload.value }}
      />
    );
  }

  return <></>;
};

export default TimeXAxisTick;
