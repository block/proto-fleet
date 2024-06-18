interface AxisTickOffsetProps {
  chartType?: "line" | "bar";
  firstTick: boolean;
  midTick: boolean;
  lastTick: boolean;
  payloadOffset: number;
  x: number;
}

const offsets = {
  line: {
    first: 25,
    mid: 16,
    last: 0,
  },
  bar: {
    first: 17,
    mid: 15,
    last: 15,
  },
};

export const getAxisTickOffset = ({
  chartType = "line",
  firstTick,
  midTick,
  lastTick,
  payloadOffset,
  x,
}: AxisTickOffsetProps) => {
  let xOffset = 0;
  const isLineChart = chartType === "line";
  if (firstTick) {
    // the offset needed to add margin left to the first tick
    xOffset = isLineChart
      ? offsets.line.first + payloadOffset
      : x - (offsets.bar.first + payloadOffset);
  } else if (midTick) {
    // the offset needed to center the mid ticks
    xOffset = isLineChart
      ? offsets.line.mid + payloadOffset
      : offsets.bar.mid;
  } else if (lastTick) {
    // the offset needed to add margin right to the first tick
    xOffset = isLineChart
      ? offsets.line.last + payloadOffset
      : offsets.bar.last;
  }
  return xOffset;
};
