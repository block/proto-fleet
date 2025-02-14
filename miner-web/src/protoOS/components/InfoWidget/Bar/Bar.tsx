import { BarChart, Bar as BarRecharts, YAxis } from "recharts";

import { chartHeight } from "./constants";
import GradientBar from "./GradientBar";

interface BarProps {
  intensity: number;
}

const Bar = ({ intensity }: BarProps) => {
  return (
    <div className="flex flex-col space-y-1" data-testid="bar">
      <BarChart width={16} height={chartHeight} data={[{ value: intensity }]} margin={{
          top: 0,
          right: 0,
          left: 0,
          bottom: 0,
        }}>
        <YAxis domain={[0, 10]} hide />
        <BarRecharts
          dataKey="value"
          barSize={16}
          radius={[0, 0, 4, 4]}
          shape={<GradientBar chartHeight={chartHeight} intensity={intensity} />}
        />
      </BarChart>
    </div>
  );
};

export default Bar;
