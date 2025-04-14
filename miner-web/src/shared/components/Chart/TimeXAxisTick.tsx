import { getAxisTickOffset } from "./utility";
import { AxisTick } from "@/shared/components/Chart";
import {
  getDayFromEpoch,
  getMonthFromEpoch,
  getShortYearFromEpoch,
  getTimeFromEpoch,
} from "@/shared/utils/datetime";

interface TimeXAxisTickProps {
  chartType?: "line" | "bar";
  dataPointCount: number;
  maxTicksToShow: number;
  maxXPosition?: number;
  minXPosition?: number;
  payload?: { value: number; index: number; offset: number };
  tooltipDatetime?: number;
  visibleTicksCount?: number;
  x?: number;
  y?: number;
}

const TimeXAxisTick = ({
  chartType,
  dataPointCount,
  maxTicksToShow,
  maxXPosition,
  minXPosition,
  payload = { value: 0, index: 0, offset: 0 },
  tooltipDatetime,
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

  if (tooltipDatetime) {
    if (tooltipDatetime === payload.value) {
      const date = `${getMonthFromEpoch(tooltipDatetime)}/${getDayFromEpoch(tooltipDatetime)}/${getShortYearFromEpoch(tooltipDatetime)}`;
      // hide seconds from showing on xAxis
      const time = getTimeFromEpoch(tooltipDatetime).slice(0, -3);
      const max = maxXPosition || x;
      const min = minXPosition || x;

      return (
        <AxisTick
          x={Math.max(Math.min(x, max), min)}
          y={y}
          xOffset={getAxisTickOffset({
            chartType,
            firstTick,
            hasDate: true,
            midTick: !firstTick && !lastTick,
            lastTick,
            payloadOffset: payload.offset,
            x,
          })}
          payload={{
            ...payload,
            value: `${date} • ${time}`,
          }}
        />
      );
    }
  } else if (firstTick || midTick || lastTick) {
    // hide seconds from showing on xAxis
    const time = getTimeFromEpoch(payload.value).slice(0, -3);
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
        payload={{ ...payload, value: time }}
      />
    );
  }

  return <></>;
};

export default TimeXAxisTick;
