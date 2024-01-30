import {
  CartesianGrid,
  LineChart as LineChartRecharts,
  Line as LineRecharts,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import Dot from "./Dot";
import LineLabel from "./LineLabel";

interface Line {
  dataKey: string;
  stroke: string;
  strokeWidth: number;
}

export interface Data {
  time: string;
  [key: string]: number | string;
}

interface TrendChartProps {
  data: Data[];
  height: number;
  lines: Line[];
  width: number;
}

const LineChart = ({ data, height, lines, width }: TrendChartProps) => {
  return (
    <ResponsiveContainer width="100%" height="100%">
      <LineChartRecharts
        height={height}
        width={width}
        data={data}
        margin={{ right: 120, top: 20, bottom: 25 }}
      >
        <CartesianGrid opacity={0.3} vertical={false} />
        <XAxis
          dataKey="time"
          axisLine={false}
          tickLine={false}
          dy={10}
          opacity={0.5}
          fontSize={10}
          fontWeight={400}
          fontFamily="Inter"
          padding={{ left: 10, right: 10 }}
        />
        <YAxis
          axisLine={false}
          dx={-10}
          opacity={0.5}
          fontSize={10}
          fontWeight={400}
          fontFamily="Inter"
        />
        <Tooltip />
        {lines.map((line, index) => (
          <LineRecharts
            key={line.dataKey}
            type="monotone"
            dataKey={line.dataKey}
            stroke={line.stroke}
            strokeWidth={line.strokeWidth}
            dot={<Dot chartDataLength={data.length} />}
            label={
              <LineLabel
                text={line.dataKey}
                chartData={data}
                lineIndex={index}
              />
            }
            isAnimationActive={false}
          />
        ))}
      </LineChartRecharts>
    </ResponsiveContainer>
  );
};

export default LineChart;
