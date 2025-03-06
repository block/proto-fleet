import { useEffect, useMemo, useState } from "react";
import {
  CartesianGrid,
  Line,
  LineChart,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import { Hashrates } from "../types";
import {
  Hashrate1Props,
  Hashrate2Props,
  Hashrate3Props,
  LineProps,
  NullLineProps,
} from "./constants";
import HashrateTooltip, { TooltipData } from "./HashrateTooltip";
import { getChartData, getPoint } from "./utility";
import {
  ChartWrapper,
  LineCursor,
  LineDot,
  TimeXAxisTick,
  xAxisProps,
  yAxisProps,
} from "@/shared/components/Chart";
import { Duration } from "@/shared/components/DurationSelector";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface HashrateChartProps {
  duration: Duration;
  hashrate1: Hashrates;
  hashrate2: Hashrates;
  hashrate3: Hashrates;
  hashrates: Hashrates;
  highestValue?: string | number;
}

const HashrateChart = ({
  hashrate1,
  hashrate2,
  hashrate3,
  hashrates,
  highestValue,
}: HashrateChartProps) => {
  const [tooltipData, setTooltipData] = useState<TooltipData>({
    x: 0,
    y: 0,
    payload: [],
  });
  const [initChart, setInitChart] = useState(false);
  const { isDesktop, isTablet, isPhone } = useWindowDimensions();
  const chartData = useMemo(
    () => getChartData({ hashrate1, hashrate2, hashrate3, hashrates }),
    [hashrate1, hashrate2, hashrate3, hashrates],
  );

  const max =
    +(highestValue || 0) ||
    Math.max(...chartData.map((data) => data.totalHashrate));
  const nearestTen = Math.round(max / 10) * 10;
  const maxDomain = nearestTen + (max < 10 ? 5 : 20);

  useEffect(() => {
    setTimeout(() => {
      // the chart should only animate on first render
      // animation takes around 1.5s
      setInitChart(true);
    }, 1500);
  }, []);

  const firstVerticalPoint = isDesktop ? 130 : 105;
  const verticalGap = isDesktop ? 87.5 : 55;
  const verticalPoints = [...Array(9)].map((_, i) =>
    getPoint(i, firstVerticalPoint, verticalGap),
  );

  const firstHorizontalPoint = 24;
  const horizontalGap = 38;
  const horizontalPoints = [...Array(9)].map((_, i) =>
    getPoint(i, firstHorizontalPoint, horizontalGap),
  );

  const yAxisTickCount = maxDomain / 5 + 10;

  return (
    <ChartWrapper>
      <LineChart
        data={chartData}
        margin={{
          top: 0,
          right: 15,
          left: -17,
          bottom: 5,
        }}
      >
        <CartesianGrid
          strokeOpacity={0.2}
          color="black"
          verticalPoints={[43, ...verticalPoints]}
          horizontalPoints={[...horizontalPoints, 365]}
        />
        <XAxis
          {...xAxisProps}
          tickMargin={28}
          tick={
            <TimeXAxisTick
              tooltipDatetime={tooltipData.payload[0]?.payload.datetime}
              dataPointCount={chartData.length}
              maxTicksToShow={isDesktop ? 13 : isTablet ? 10 : 5}
              minXPosition={85}
              maxXPosition={isPhone ? 303 : 871}
            />
          }
        />
        <YAxis
          {...yAxisProps}
          padding={{ top: -10, bottom: 0 }}
          domain={[0, maxDomain]}
          tickCount={Math.min(15, yAxisTickCount)}
        />
        <Tooltip
          position={{ y: tooltipData.y - 150, x: tooltipData.x - 290 }}
          content={
            <HashrateTooltip
              onHover={setTooltipData}
              tooltipData={tooltipData}
            />
          }
          cursor={<LineCursor />}
          isAnimationActive={false}
        />
        {!!tooltipData.payload.length && (
          <>
            {hashrate1.length && <Line {...LineProps} {...Hashrate1Props} />}
            {hashrate1.length && (
              <Line
                {...LineProps}
                {...Hashrate1Props}
                {...NullLineProps}
                strokeOpacity={0.5}
              />
            )}
            {hashrate2.length && <Line {...LineProps} {...Hashrate2Props} />}
            {hashrate2.length && (
              <Line
                {...LineProps}
                {...Hashrate2Props}
                {...NullLineProps}
                strokeOpacity={0.5}
              />
            )}
            {hashrate3.length && <Line {...LineProps} {...Hashrate3Props} />}
            {hashrate3.length && (
              <Line
                {...LineProps}
                {...Hashrate3Props}
                {...NullLineProps}
                strokeOpacity={0.8}
              />
            )}
          </>
        )}
        <Line
          {...LineProps}
          dataKey="totalHashrate"
          stroke="currentColor"
          className="text-intent-warning-fill"
          activeDot={
            tooltipData.payload.length ? (
              <LineDot fillClassName="fill-intent-warning-fill" />
            ) : (
              <></>
            )
          }
          isAnimationActive={!initChart}
        />
      </LineChart>
    </ChartWrapper>
  );
};

export default HashrateChart;
