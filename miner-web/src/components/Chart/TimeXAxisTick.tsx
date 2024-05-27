import { AxisTick } from "components/Chart";

import { getAxisTickOffset } from "./utility";

interface TimeXAxisTickProps {
  chartType?: "line" | "bar";
  payload?: { value: string; index: number; offset: number };
  tooltipTime?: string;
  visibleTicksCount?: number;
  x?: number;
  y?: number;
}

const TimeXAxisTick = ({
  chartType,
  payload = { value: "", index: 0, offset: 0 },
  tooltipTime,
  visibleTicksCount = 0,
  x = 0,
  y = 0,
}: TimeXAxisTickProps) => {
  const { index } = payload;
  const firstTick = index === 0;
  const lastTick = index === visibleTicksCount - 1;
  // show time for every 6th tick but maintain more than two tick gap before last tick
  const midTick = !firstTick && !lastTick && index % 6 === 0 && index < visibleTicksCount - 2;
  if (tooltipTime) {
    if (tooltipTime === payload.value) {
      return (
        <AxisTick
          x={x}
          y={y}
          xOffset={getAxisTickOffset({
            chartType,
            firstTick,
            midTick: !firstTick && !lastTick,
            lastTick,
            payloadOffset: payload.offset,
          })}
          payload={{ ...payload, value: payload.value }}
        />
      );
    }
  } else if (firstTick || midTick || lastTick) {
    return (
      <AxisTick
        x={x}
        y={y}
        xOffset={getAxisTickOffset({
          chartType,
          firstTick,
          midTick,
          lastTick,
          payloadOffset: payload.offset,
        })}
        payload={{ ...payload, value: payload.value }}
      />
    );
  }

  return <></>;
};

export default TimeXAxisTick;
