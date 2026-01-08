import { useMemo } from "react";
import { Line, LineChart, ReferenceLine, ResponsiveContainer, YAxis } from "recharts";
import useCssVariable from "@/shared/hooks/useCssVariable";

type SparklineProps = {
  threshold?: number;
  data: {
    time: number;
    y: number;
  }[];
};

const Sparkline = ({ data, threshold = 2 }: SparklineProps) => {
  const gray = useCssVariable("--color-core-primary-10");
  const critical = useCssVariable("--color-text-critical");
  const success = useCssVariable("--color-text-success");
  const warning = useCssVariable("--color-text-warning");
  const neutral = useCssVariable("--color-text-primary-30");

  const { lineColor, startValue, minValue, maxValue } = useMemo(() => {
    const _data = data.sort((d) => d.time);
    const startValue = _data[0]?.y;
    const endValue = _data[data.length - 1].y;

    const allValues = _data.map((item) => item.y);
    const minValue = Math.min(...allValues);
    const maxValue = Math.max(...allValues);

    let lineColor = gray;
    if (endValue >= startValue) {
      lineColor = endValue - startValue > threshold ? success : neutral;
    } else {
      lineColor = startValue - endValue > threshold ? critical : warning;
    }

    return {
      startValue,
      minValue,
      maxValue,
      lineColor,
    };
  }, [data, gray, critical, success, warning, neutral, threshold]);

  return (
    <ResponsiveContainer
      width="100%"
      height="100%"
      minWidth={"10px"}
      minHeight={"10px"}
      className="pointer-events-none"
    >
      <LineChart data={data}>
        <YAxis domain={[minValue, maxValue]} hide={true} />
        <ReferenceLine y={startValue} stroke={gray} />
        <Line type="monotone" dataKey="y" stroke={lineColor} dot={false} isAnimationActive={false} />
      </LineChart>
    </ResponsiveContainer>
  );
};

export default Sparkline;
