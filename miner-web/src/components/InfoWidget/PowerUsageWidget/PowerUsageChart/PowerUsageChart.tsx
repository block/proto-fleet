import { useState } from "react";
import { Bar, BarChart, Tooltip, XAxis, YAxis } from "recharts";

import { useWindowDimensions } from "common/hooks/useWindowDimensions";

import {
  ChartWrapper,
  TimeXAxisTick,
  xAxisProps,
  yAxisProps,
} from "components/Chart";

import TickTooltip, { TooltipData } from "../../common/TickTooltip";
import PowerUsageBar from "./PowerUsageBar";

interface PowerUsageChartProps {
  powers: Record<string, number | string>[];
  maxPower: number;
}

const PowerUsageChart = ({ maxPower, powers }: PowerUsageChartProps) => {
  const [tooltipData, setTooltipData] = useState<TooltipData>({
    x: 0,
    y: 0,
    payload: [],
  });
  const { isPhone } = useWindowDimensions();

  return (
    <ChartWrapper>
      <BarChart
        data={powers}
        margin={{
          top: 16,
          right: 16,
          left: -30,
          bottom: 0,
        }}
      >
        <XAxis
          {...xAxisProps}
          tick={
            <TimeXAxisTick
              tooltipDatetime={tooltipData.payload[0]?.payload.datetime}
              dataPointCount={powers.length}
              maxTicksToShow={isPhone ? 5 : 10}
              minXPosition={75}
              maxXPosition={isPhone ? 303 : 536}
              chartType="bar"
            />
          }
        />
        <YAxis
          {...yAxisProps}
          scale="linear"
          tickMargin={6}
          domain={[0, maxPower + 1]}
          padding={{ top: -5, bottom: 5 }}
        />
        <Tooltip
          cursor={{ fill: "currentColor", className: "text-surface-elevated-base" }}
          position={{ y: -45, x: tooltipData.x - 30 }}
          content={
            <TickTooltip
              onHover={setTooltipData}
              tooltipData={tooltipData}
              unit="kW"
            />
          }
          isAnimationActive={false}
        />
        <Bar
          dataKey="value"
          barSize={16}
          radius={[0, 0, 4, 4]}
          shape={<PowerUsageBar />}
          activeBar={<PowerUsageBar active={!!tooltipData.payload.length} />}
        />
      </BarChart>
    </ChartWrapper>
  );
};

export default PowerUsageChart;
