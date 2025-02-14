import { Pie, PieChart, ResponsiveContainer } from "recharts";

interface FanSpeedPieChartProps {
  acceptableSpeed: number;
  fanSpeed: number;
  maxSpeed: number;
}

const FanSpeedPieChart = ({
  acceptableSpeed,
  fanSpeed,
  maxSpeed,
}: FanSpeedPieChartProps) => {
  const data = [
    { name: "A", value: fanSpeed, fillOpacity: 1 },
    { name: "B", value: maxSpeed - fanSpeed },
  ];

  return (
    <div data-testid="fan-speed-pie-chart" className="w-full h-full">
      <ResponsiveContainer width="100%" height="100%">
        <PieChart>
          <Pie
            cx={15}
            cy={15}
            data={data}
            dataKey="value"
            fill="currentColor"
            className={
              fanSpeed < acceptableSpeed
                ? "text-intent-warning-fill"
                : "text-intent-success-fill"
            }
            fillOpacity={0.5}
            innerRadius={10}
            outerRadius={20}
            stroke="none"
            startAngle={90}
            endAngle={-270}
            isAnimationActive={false}
          />
        </PieChart>
      </ResponsiveContainer>
    </div>
  );
};

export default FanSpeedPieChart;
