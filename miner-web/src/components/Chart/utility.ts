interface AxisTickOffsetProps {
  chartType?: "line" | "bar";
  firstTick: boolean;
  midTick: boolean;
  lastTick: boolean;
  payloadOffset: number;
}

export const getAxisTickOffset = ({
  chartType = "line",
  firstTick,
  midTick,
  lastTick,
  payloadOffset,
}: AxisTickOffsetProps) => {
  let xOffset = 0;
  if (firstTick) {
    // the offset needed to add margin left to the first tick
    xOffset = 25 - payloadOffset;
  } else if (midTick) {
    // the offset needed to center the mid ticks
    const midTickOffset = chartType === "line" ? 16 : 0;
    xOffset = midTickOffset + payloadOffset;
  } else if (lastTick) {
    // the offset needed to add margin right to the first tick
    xOffset = 0 + payloadOffset;
  }
  return xOffset;
};
