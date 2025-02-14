import { useState } from "react";
import {
  CartesianGrid,
  Line,
  LineChart,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import TickTooltip, { TooltipData } from "../common/TickTooltip";
import {
  ChartWrapper,
  LineCursor,
  LineDot,
  TimeXAxisTick,
  xAxisProps,
  yAxisProps,
} from "@/shared/components/Chart";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface EfficiencyChartProps {
  efficiencies: Record<string, number | string>[];
}

const EfficiencyChart = ({ efficiencies }: EfficiencyChartProps) => {
  const [tooltipData, setTooltipData] = useState<TooltipData>({
    x: 0,
    y: 0,
    payload: [],
  });
  const { isPhone } = useWindowDimensions();

  return (
    <ChartWrapper>
      <LineChart
        data={efficiencies}
        margin={{
          top: 15,
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
            <TimeXAxisTick
              tooltipDatetime={tooltipData.payload[0]?.payload.datetime}
              dataPointCount={efficiencies.length}
              maxTicksToShow={isPhone ? 5 : 10}
              minXPosition={85}
              maxXPosition={isPhone ? 280 : 520}
            />
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
          className="text-intent-success-fill"
          stroke="currentColor"
          strokeWidth={2.5}
          label={false}
          dot={false}
          strokeLinecap="round"
          strokeLinejoin="round"
          activeDot={
            tooltipData.payload.length ? (
              <LineDot fillClassName="fill-intent-success-fill" />
            ) : (
              <></>
            )
          }
        />
      </LineChart>
    </ChartWrapper>
  );
};

export default EfficiencyChart;
