import { LineChart, Line as RechartsLine } from "recharts";

interface LineProps {
  data: Record<"value", number>[];
}

const Line = ({ data }: LineProps) => {
  return (
    <div className="absolute right-4" data-testid="line">
      <LineChart width={120} height={64} data={data}>
        <RechartsLine
          type="monotone"
          dataKey="value"
          stroke="#38A600"
          strokeWidth={2}
          label={false}
          dot={false}
          className="hover:cursor-pointer"
        />
      </LineChart>
      <div className="absolute bottom-0 h-full w-full pointer-events-none bg-gradient-to-r from-surface-base to-transparent" />
    </div>
  );
};

export default Line;
