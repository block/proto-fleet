import { getAxisTickOffset } from "./utility";
import { AxisTick } from "@/shared/components/Chart";
import { getDayFromEpoch, getMonthFromEpoch, getShortYearFromEpoch, getTimeFromEpoch } from "@/shared/utils/datetime";

interface TimeXAxisTickProps {
  chartType?: "line" | "bar";
  dataPointCount: number;
  hideNonTooltipTicks?: boolean;
  maxTicksToShow: number;
  maxXPosition?: number;
  minXPosition?: number;
  payload?: { value: number; index: number; offset: number };
  showDate?: boolean;
  tooltipDatetime?: number;
  tooltipTickValue?: number;
  visibleTicksCount?: number;
  x?: number;
  y?: number;
  labelCount?: number;
  timeBasedIndices?: number[];
}

const TimeXAxisTick = ({
  chartType,
  dataPointCount,
  hideNonTooltipTicks,
  maxTicksToShow,
  maxXPosition,
  minXPosition,
  payload = { value: 0, index: 0, offset: 0 },
  showDate,
  tooltipDatetime,
  tooltipTickValue,
  visibleTicksCount = 0,
  x = 0,
  y = 0,
  labelCount,
  timeBasedIndices,
}: TimeXAxisTickProps) => {
  const { index } = payload;
  let firstTick = index === 0;
  let lastTick = index === visibleTicksCount - 1;
  let midTick: boolean;

  // Calculate which ticks to show based on mode
  if (timeBasedIndices && timeBasedIndices.length > 0) {
    // Use time-based indices for evenly spaced labels in time
    if (timeBasedIndices.includes(index)) {
      // Determine if this is first, middle, or last label
      firstTick = index === timeBasedIndices[0];
      lastTick = index === timeBasedIndices[timeBasedIndices.length - 1];
      midTick = !firstTick && !lastTick;
    } else {
      // Not a target index, don't show this tick
      firstTick = false;
      lastTick = false;
      midTick = false;
    }
  } else if (labelCount) {
    // Fallback to index-based spacing if time-based indices not available
    // Handle edge case: if only 1 label, show first tick only
    if (labelCount <= 1) {
      firstTick = index === 0;
      lastTick = false;
      midTick = false;
    } else {
      const segmentSize = Math.floor((visibleTicksCount - 1) / (labelCount - 1));
      const targetIndices = Array.from({ length: labelCount }, (_, i) => i * segmentSize);

      if (targetIndices.includes(index)) {
        // Determine if this is first, middle, or last label
        firstTick = index === targetIndices[0];
        lastTick = index === targetIndices[targetIndices.length - 1];
        midTick = !firstTick && !lastTick;
      } else {
        // Not a target index, don't show this tick
        firstTick = false;
        lastTick = false;
        midTick = false;
      }
    }
  } else {
    // show a max number of ticks on the x-axis to avoid overlapping time labels
    const showEveryNthTick = Math.ceil(dataPointCount / maxTicksToShow);
    // show time for every nth tick and maintain more than nth tick gap before last tick
    midTick = !firstTick && !lastTick && index % showEveryNthTick === 0 && index < visibleTicksCount - showEveryNthTick;
  }

  const hasTooltipDatetime = tooltipDatetime !== undefined;
  const targetTooltipTickValue = tooltipTickValue ?? tooltipDatetime;

  if (hasTooltipDatetime && targetTooltipTickValue === payload.value) {
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
  } else if (hasTooltipDatetime && hideNonTooltipTicks) {
    return null;
  } else if (firstTick || midTick || (lastTick && !timeBasedIndices)) {
    // hide seconds from showing on xAxis
    // Note: When using timeBasedIndices, hide the last tick to leave space for current time
    const time = getTimeFromEpoch(payload.value).slice(0, -3);
    const label = showDate ? `${getMonthFromEpoch(payload.value)}/${getDayFromEpoch(payload.value)} ${time}` : time;
    return (
      <AxisTick
        x={x}
        y={y}
        xOffset={getAxisTickOffset({
          chartType,
          firstTick,
          hasDate: showDate,
          midTick,
          lastTick,
          payloadOffset: payload.offset,
          x,
        })}
        payload={{ ...payload, value: label }}
      />
    );
  }

  return null;
};

export default TimeXAxisTick;
