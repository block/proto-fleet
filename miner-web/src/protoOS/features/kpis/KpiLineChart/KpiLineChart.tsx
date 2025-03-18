import { useEffect, useMemo, useState } from "react";
import {
  CartesianGrid,
  Line,
  LineChart,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import { lineColors, lineProps } from "./constants";

import KpiTooltip, { type TooltipData } from "./KpiTooltip";
import { type TimeSeries } from "./types";
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

interface KpiChartProps {
  duration: Duration;
  series: TimeSeries[];
  units?: string;
  aggregateSeries: TimeSeries;
  highestValue?: string | number;
}

const KpiChart = ({
  series,
  aggregateSeries,
  highestValue,
  units,
}: KpiChartProps) => {
  const [tooltipData, setTooltipData] = useState<TooltipData>({
    x: 0,
    y: 0,
    payload: [],
  });

  const [initChart, setInitChart] = useState(false);
  const { isDesktop, isTablet, isPhone } = useWindowDimensions();
  const chartData = useMemo(
    () => getChartData({ series, aggregateSeries, units }),
    [series, aggregateSeries, units],
  );

  // TODO: another perf bottleneck because were iterating over all data again
  // were already iterating each item in getChartData so we could just return
  // max here as well and it would be memoized
  const max =
    +(highestValue || 0) ||
    Math.max(...chartData.map((data) => data[aggregateSeries.name])) * 1.1;

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

  // const referenceDots: ReactNode[] = [];
  // horizontalPoints.forEach((x) => {
  //   verticalPoints.forEach((y) => {
  //     referenceDots.push(
  //       <ReferenceDot
  //         key={`${x}-${y}`}
  //         x={x}
  //         y={y}
  //         r={2}
  //         fill="#fff"
  //         stroke="none"
  //       />,
  //     );
  //   });
  // });

  return (
    <ChartWrapper>
      <LineChart
        data={chartData}
        margin={{
          top: 0,
          right: 0,
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
            <KpiTooltip
              onHover={setTooltipData}
              tooltipData={tooltipData}
              units={units}
            />
          }
          cursor={<LineCursor />}
          isAnimationActive={false}
        />
        {!!tooltipData.payload.length && (
          <>
            {series.map((seriesItem, index) => {
              if (seriesItem.data.length) {
                return (
                  <Line
                    {...lineProps}
                    dataKey={seriesItem.name}
                    key={index}
                    isAnimationActive={false}
                    stroke={lineColors[index % lineColors.length]}
                  />
                );
              }
            })}
          </>
        )}
        <Line
          {...lineProps}
          dataKey={aggregateSeries.name}
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

export default KpiChart;
