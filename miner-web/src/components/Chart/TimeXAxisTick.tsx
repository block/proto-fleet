import { AxisTick } from "components/Chart";

interface TimeXAxisTickProps {
  payload: { value: string; index: number; offset: number };
  x: number;
  y: number;
}

const TimeXAxisTick = ({ x, y, payload }: TimeXAxisTickProps) => {
  const { index } = payload;
  const firstTick = index === 0;
  const midTick = index === 12;
  const lastTick = index === 23;
  if (firstTick || midTick || lastTick) {
    let xOffset = 0;
    if (firstTick) {
      // the offset needed to add margin left to the first tick
      xOffset = 25 - payload.offset;
    } else if (midTick) {
      // the offset needed to center the mid tick
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
        payload={{ ...payload, value: index === 23 ? "Now" : payload.value }}
      />
    );
  }

  return <></>;
};

export default TimeXAxisTick;
