import { useMemo, useState } from "react";
import {
  CartesianGrid,
  Line,
  LineChart,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import {
  ChartWrapper,
  LineCursor,
  LineDot,
  TimeXAxisTick,
  xAxisProps,
  yAxisProps,
} from "components/Chart";

import TickTooltip, { TooltipData } from "../common/TickTooltip";
import { getChartData } from "./constants";

const EfficiencyChart = () => {
  const [tooltipData, setTooltipData] = useState<TooltipData>({
    x: 0,
    y: 0,
    payload: [],
  });

  const chartData = useMemo(() => getChartData(), []);

  return (
    <ChartWrapper>
      <LineChart
        data={chartData}
        margin={{
          top: 0,
          right: 30,
          left: -17,
          bottom: 5,
        }}
      >
        <CartesianGrid
          strokeOpacity={0.2}
          color="black"
          verticalPoints={[42.5, 100, 150, 200, 250, 300, 350, 400, 450, 500]}
          horizontalPoints={[30, 70, 110, 150, 192.5]}
        />
        <XAxis
          {...xAxisProps}
          tick={
            <TimeXAxisTick tooltipTime={tooltipData.payload[0]?.payload.time} />
          }
        />
        <YAxis {...yAxisProps} padding={{ top: -5, bottom: 0 }} />
        <Tooltip
          position={{ y: tooltipData.y - 33, x: tooltipData.x - 100 }}
          content={
            <TickTooltip
              onHover={setTooltipData}
              tooltipData={tooltipData}
              unit="J/TH"
            />
          }
          cursor={<LineCursor />}
          isAnimationActive={false}
        />
        <Line
          type="monotone"
          dataKey="value"
          stroke="#38A600"
          strokeWidth={2.5}
          label={false}
          dot={false}
          strokeLinecap="round"
          strokeLinejoin="round"
          activeDot={
            tooltipData.payload.length ? <LineDot color="#38A600" /> : <></>
          }
        />
      </LineChart>
    </ChartWrapper>
  );
};

export default EfficiencyChart;
