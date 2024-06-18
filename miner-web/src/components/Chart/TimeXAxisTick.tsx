import { AxisTick } from "components/Chart";

import { getAxisTickOffset } from "./utility";

interface TimeXAxisTickProps {
  chartType?: "line" | "bar";
  dataPointCount: number;
  maxTicksToShow: number;
  payload?: { value: string; index: number; offset: number };
  tooltipTime?: string;
  visibleTicksCount?: number;
  x?: number;
  y?: number;
}

const TimeXAxisTick = ({
  chartType,
  dataPointCount,
  maxTicksToShow,
  payload = { value: "", index: 0, offset: 0 },
  tooltipTime,
  visibleTicksCount = 0,
  x = 0,
  y = 0,
}: TimeXAxisTickProps) => {
  const { index } = payload;
  const firstTick = index === 0;
  const lastTick = index === visibleTicksCount - 1;
  // show a max number of ticks on the x-axis to avoid overlapping time labels
  const showEveryNthTick = Math.ceil(dataPointCount / maxTicksToShow);
  // show time for every nth tick and maintain more than nth tick gap before last tick
  const midTick =
    !firstTick &&
    !lastTick &&
    index % showEveryNthTick === 0 &&
    index < visibleTicksCount - showEveryNthTick;
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
            x,
          })}
          // hide seconds from showing on xAxis
          payload={{ ...payload, value: payload.value.slice(0, 5) }}
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
          x,
        })}
        // hide seconds from showing on xAxis
        payload={{ ...payload, value: payload.value.slice(0, 5) }}
      />
    );
  }

  return <></>;
};

export default TimeXAxisTick;
