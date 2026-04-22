import { useEffect, useMemo, useState } from "react";
import { Line, LineChart, Tooltip, XAxis } from "recharts";

import AsicChartTooltip, { TooltipData } from "./AsicChartTooltip";
import { hashrateLineProps, nullLineProps, temperatureLineProps } from "./constants";
import { getChartData } from "./utility";
import { ChartWrapper, LineCursor, LineDot, TimeXAxisTick, xAxisProps } from "@/shared/components/Chart";
import { ChartData } from "@/shared/components/LineChart/types";

const ANIMATION_DURATION = 500;

interface AsicChartProps {
  hashrateData: ChartData[];
  temperatureData: ChartData[];
}

const AsicChart = ({ hashrateData, temperatureData }: AsicChartProps) => {
  const [tooltipData, setTooltipData] = useState<TooltipData>({
    x: 0,
    y: 0,
    payload: [],
  });

  const [shouldAnimate, setShouldAnimate] = useState(true);

  const chartData = useMemo(() => {
    return getChartData({ hashrateData, temperatureData });
  }, [hashrateData, temperatureData]);

  // initialize animation flags
  useEffect(() => {
    setShouldAnimate(true);
    const timeoutId = setTimeout(() => {
      setShouldAnimate(false);
    }, ANIMATION_DURATION);
    return () => clearTimeout(timeoutId);
  }, []);

  return (
    <ChartWrapper>
      <LineChart
        data={chartData}
        margin={{
          top: 10,
          right: 12,
          left: 12,
          bottom: 0,
        }}
      >
        <XAxis
          {...xAxisProps}
          tick={
            <TimeXAxisTick
              tooltipDatetime={tooltipData.payload[0]?.payload.datetime}
              dataPointCount={hashrateData?.length}
              maxTicksToShow={5}
              minXPosition={60}
              maxXPosition={220}
            />
          }
        />
        <Tooltip
          position={{ y: tooltipData.y - 150, x: tooltipData.x - 90 }}
          content={<AsicChartTooltip onHover={setTooltipData} tooltipData={tooltipData} />}
          cursor={<LineCursor />}
          isAnimationActive={false}
        />
        <Line {...hashrateLineProps} {...nullLineProps} activeDot={<></>} strokeOpacity={0.5} />
        <Line
          {...hashrateLineProps}
          activeDot={
            tooltipData.payload[0]?.payload.hashrate_ghs !== undefined ? (
              <LineDot fillClassName="fill-core-primary-fill" />
            ) : (
              <></>
            )
          }
          isAnimationActive={shouldAnimate}
          animationDuration={ANIMATION_DURATION}
        />
        <Line {...temperatureLineProps} {...nullLineProps} activeDot={<></>} strokeOpacity={0.5} />
        <Line
          {...temperatureLineProps}
          activeDot={
            tooltipData.payload[0]?.payload.temp_c !== undefined ? (
              <LineDot fillClassName="fill-core-accent-fill" />
            ) : (
              <></>
            )
          }
          isAnimationActive={shouldAnimate}
          animationDuration={ANIMATION_DURATION}
        />
      </LineChart>
    </ChartWrapper>
  );
};

export default AsicChart;
