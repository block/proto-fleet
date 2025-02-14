import { LineChart, Line as RechartsLine } from "recharts";
import clsx from "clsx";

import "./style.css";

interface LineProps {
  data: Record<string, number | string>[];
}

const Line = ({ data }: LineProps) => {
  return (
    <div className="absolute right-4" data-testid="line">
      <LineChart width={120} height={64} data={data}>
        <RechartsLine
          type="monotone"
          dataKey="value"
          stroke="currentColor"
          strokeWidth={2}
          label={false}
          dot={false}
          className="text-intent-success-fill hover:cursor-pointer"
        />
      </LineChart>
      <div
        className={clsx(
          "absolute bottom-0 h-full w-full pointer-events-none",
          "transition-gradient"
        )}
      />
    </div>
  );
};

export default Line;
