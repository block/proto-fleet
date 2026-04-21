interface AxisTickOffsetProps {
  chartType?: "line" | "bar";
  firstTick: boolean;
  hasDate?: boolean;
  midTick: boolean;
  lastTick: boolean;
  payloadOffset: number;
  x: number;
}

const offsets = {
  line: {
    first: 18,
    firstDate: 28,
    mid: 16,
    midDate: 25,
    last: 0,
    lastDate: 0,
  },
  bar: {
    first: 17,
    firstDate: 26,
    mid: 15,
    midDate: 24,
    last: 15,
    lastDate: 24,
  },
};

export const getAxisTickOffset = ({
  chartType = "line",
  firstTick,
  hasDate,
  midTick,
  lastTick,
  payloadOffset,
  x,
}: AxisTickOffsetProps) => {
  let xOffset = 0;
  const isLineChart = chartType === "line";
  if (firstTick) {
    // the offset needed to add margin left to the first tick
    if (isLineChart) {
      const dateOffset = hasDate ? offsets.line.firstDate : 0;
      xOffset = offsets.line.first + payloadOffset + dateOffset;
    } else {
      const dateOffset = hasDate ? offsets.bar.firstDate : 0;
      xOffset = x - (offsets.bar.first + payloadOffset) + dateOffset;
    }
  } else if (midTick) {
    // the offset needed to center the mid ticks
    if (isLineChart) {
      const dateOffset = hasDate ? offsets.line.midDate : 0;
      xOffset = offsets.line.mid + payloadOffset + dateOffset;
    } else {
      const dateOffset = hasDate ? offsets.bar.midDate : 0;
      xOffset = offsets.bar.mid + dateOffset;
    }
  } else if (lastTick) {
    // the offset needed to add margin right to the first tick
    if (isLineChart) {
      const dateOffset = hasDate ? offsets.line.lastDate : 0;
      xOffset = offsets.line.last + payloadOffset + dateOffset;
    } else {
      const dateOffset = hasDate ? offsets.bar.lastDate : 0;
      xOffset = offsets.bar.last + dateOffset;
    }
  }
  return xOffset;
};
