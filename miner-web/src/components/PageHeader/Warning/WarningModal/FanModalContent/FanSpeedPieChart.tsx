import { Pie, PieChart, ResponsiveContainer } from "recharts";

interface FanSpeedPieChartProps {
  fanSpeed: number;
  maxSpeed: number;
}

const FanSpeedPieChart = ({ fanSpeed, maxSpeed }: FanSpeedPieChartProps) => {
  const data = [
    { name: "A", value: fanSpeed },
    { name: "B", value: maxSpeed - fanSpeed, fillOpacity: 1 },
  ];

  return (
    <ResponsiveContainer width="100%" height="100%">
      <PieChart>
        <Pie
          cx={15}
          cy={15}
          data={data}
          dataKey="value"
          fill="#FD8A00"
          fillOpacity={0.5}
          innerRadius={10}
          outerRadius={20}
          stroke="none"
          startAngle={-270}
          endAngle={90}
          isAnimationActive={false}
        />
      </PieChart>
    </ResponsiveContainer>
  );
};

export default FanSpeedPieChart;
