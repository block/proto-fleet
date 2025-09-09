import { useEffect, useState, useMemo } from "react";
import {
  CartesianGrid,
  Line,
  LineChart,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import { lineProps } from "./constants";

import KpiTooltip, {
  type HashboardLocationStore,
  type TooltipData,
} from "./KpiTooltip";

import { type TimeSeries, type TimeSeriesWithSerial } from "./types";
import { type ChartData, getChartData } from "./utility";
import {
  ChartWrapper,
  LineCursor,
  TimeXAxisTick,
  xAxisProps,
  yAxisProps,
} from "@/shared/components/Chart";
import useCssVariable from "@/shared/hooks/useCssVariable";
import useMeasure from "@/shared/hooks/useMeasure";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const ANIMATION_DURATION = 1500;
const TOOLTIP_WIDTH = 269;
const TOOLTIP_WIDTH_PHONE = 150;
const TOOLTIP_OFFSET = 24;
const Y_AXIS_TICK_WIDTH = 43;

export interface KpiChartProps {
  series: TimeSeriesWithSerial[];
  units?: string;
  aggregateSeries: TimeSeries;
  showAggregate?: boolean;
  activeSeries?: TimeSeriesWithSerial["serial"][];
  highestValue?: string | number;
  hashboardLocationStore?: HashboardLocationStore;
  tickCount?: number;
  minTickInterval?: number;
  getSeriesColorMap?: (series: TimeSeriesWithSerial[]) => {
    [key: string]: string;
  };
}

const KpiChart = ({
  series,
  aggregateSeries,
  showAggregate = true,
  activeSeries,
  highestValue,
  units,
  tickCount = 10,
  minTickInterval = 0.5,
  hashboardLocationStore,
  getSeriesColorMap,
}: KpiChartProps) => {
  const [chartRef, _, chartBoundingRect] = useMeasure<HTMLDivElement>();
  const [tooltipData, setTooltipData] = useState<TooltipData>({
    x: 0,
    y: 0,
    payload: [],
  });

  const corePrimary5 = useCssVariable("--color-core-primary-5");

  const [shouldAnimate, setShouldAnimate] = useState(true);
  const { isDesktop, isTablet, isLaptop, isPhone } = useWindowDimensions();
  const [chartData, setChartData] = useState<ChartData[] | null>(null);
  const [maxDomain, setMaxDomain] = useState<number>(0);
  const [minDomain, setMinDomain] = useState<number>(0);
  const [yAxisTicks, setYAxisTicks] = useState<number[]>([]);

  const [seriesColors, setSeriesColors] = useState<{
    [key: string]: string;
  } | null>(null);

  useEffect(() => {
    if (!getSeriesColorMap) return;
    setSeriesColors(getSeriesColorMap(series));
  }, [series, getSeriesColorMap]);

  // initialize animation flags and chart data
  useEffect(() => {
    setShouldAnimate(true);
    setChartData(() =>
      getChartData({
        series,
        aggregateSeries,
        units,
      }),
    );
    const timeoutId = setTimeout(() => {
      setShouldAnimate(false);
    }, ANIMATION_DURATION);
    return () => clearTimeout(timeoutId);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // any time we receive new data update the chart data
  // except for during an animation
  useEffect(() => {
    if (shouldAnimate) {
      return;
    }

    setChartData(
      getChartData({
        series,
        aggregateSeries,
        units,
      }),
    );
  }, [series, aggregateSeries, units, shouldAnimate]);

  useEffect(() => {
    if (!chartData?.length) return;

    const max =
      +(highestValue || 0) ||
      Math.max(
        ...chartData.map((data) => {
          return Math.max(
            ...Object.entries(data)
              .filter(
                ([key, _]) =>
                  activeSeries?.includes(key) ||
                  (key === aggregateSeries.name && showAggregate) ||
                  // if all series are inactive and show aggregate is false set
                  // max according to the aggregate
                  (!activeSeries?.length && key === aggregateSeries.name),
              )
              .map(([_, value]) => value)
              .filter((v) => typeof v === "number"),
          );
        }),
      );

    const min =
      +(highestValue || 0) ||
      Math.min(
        ...chartData.map((data) => {
          return Math.min(
            ...Object.entries(data)
              .filter(
                ([key, _]) =>
                  activeSeries?.includes(key) ||
                  (key === aggregateSeries.name && showAggregate) ||
                  // if all series are inactive and show aggregate is false set
                  // max according to the aggregate
                  (!activeSeries?.length && key === aggregateSeries.name),
              )
              .map(([_, value]) => value)
              .filter((v) => typeof v === "number"),
          );
        }),
      );

    const range = max - min;
    const paddedMin = min - range * 0.2;
    const paddedMax = max + range * 0.2;

    const tickInterval = Math.max(
      Math.round(
        ((paddedMax - paddedMin) / (tickCount - 1)) * (1 / minTickInterval),
      ) /
        (1 / minTickInterval),
      minTickInterval,
    );
    const middleTick =
      Math.round(((paddedMin + paddedMax) / 2) * (1 / minTickInterval)) /
      (1 / minTickInterval);

    let ticks = Array.from(
      { length: tickCount },
      (_, i) => middleTick - (tickCount / 2 - 1 - i) * tickInterval,
    ).sort((a, b) => a - b);

    // If any ticks are negative, shift the whole array so first tick is 0
    if (ticks[0] < 0) {
      const shift = -ticks[0];
      ticks = ticks.map((tick) => tick + shift);
    }

    setMinDomain(ticks[0]);
    setMaxDomain(ticks[ticks.length - 1]);
    setYAxisTicks(ticks);
  }, [
    chartData,
    tickCount,
    minTickInterval,
    highestValue,
    aggregateSeries.name,
    activeSeries,
    showAggregate,
  ]);

  const toolTipWidth = useMemo(() => {
    return isPhone ? TOOLTIP_WIDTH_PHONE : TOOLTIP_WIDTH;
  }, [isPhone]);

  const toolTipPositionX = useMemo(() => {
    const cursorIsLeftSide =
      tooltipData.x < Y_AXIS_TICK_WIDTH + chartBoundingRect.width / 2;

    if (cursorIsLeftSide) {
      // position tooltip on the right side
      return chartBoundingRect.width - TOOLTIP_OFFSET - toolTipWidth;
    } else {
      // position tooltip on the left side
      return TOOLTIP_OFFSET + Y_AXIS_TICK_WIDTH;
    }
  }, [tooltipData.x, chartBoundingRect.width, isPhone, toolTipWidth]);

  return (
    <div ref={chartRef} className="min-h-100 flex-1">
      <ChartWrapper className="mb-10 h-full w-full">
        {chartData?.length ? (
          <LineChart
            data={chartData || []}
            margin={{
              top: 0,
              right: 0,
              left: -17,
              bottom: 5,
            }}
          >
            <CartesianGrid stroke="#eee" strokeWidth={1} vertical={false} />

            <XAxis
              {...xAxisProps}
              tickMargin={28}
              axisLine={{ stroke: corePrimary5, strokeWidth: 1 }}
              dataKey="datetime"
              scale="time"
              tick={
                <TimeXAxisTick
                  tooltipDatetime={tooltipData.payload[0]?.payload.datetime}
                  dataPointCount={chartData?.length || 0}
                  maxTicksToShow={
                    isDesktop ? 13 : isLaptop ? 10 : isTablet ? 8 : 6
                  }
                  minXPosition={85}
                  maxXPosition={isPhone ? 303 : 871}
                />
              }
            />

            <YAxis
              {...yAxisProps}
              axisLine={{ stroke: corePrimary5, strokeWidth: 1 }}
              domain={[minDomain, maxDomain]}
              ticks={yAxisTicks}
              allowDecimals={false}
              allowDataOverflow
            />

            <Tooltip
              position={{
                y: TOOLTIP_OFFSET,
                x: toolTipPositionX,
              }}
              wrapperStyle={{ outline: "none" }}
              content={
                <KpiTooltip
                  aggregateLabel="Summary"
                  onHover={setTooltipData}
                  tooltipData={tooltipData}
                  activeSeries={activeSeries}
                  showAggregate={showAggregate}
                  units={units}
                  hashboardLocationStore={hashboardLocationStore}
                  tooltipWidth={isPhone ? TOOLTIP_WIDTH_PHONE : TOOLTIP_WIDTH}
                />
              }
              cursor={<LineCursor />}
              isAnimationActive={false}
            />

            {series
              .filter((s) => activeSeries?.includes(s.serial))
              .map((seriesItem, index) => {
                if (seriesItem.data.length) {
                  return (
                    <Line
                      {...lineProps}
                      dataKey={seriesItem.serial}
                      key={index}
                      isAnimationActive={false}
                      stroke={`var(${seriesColors?.[seriesItem.serial]})`}
                    />
                  );
                }
              })}

            {showAggregate && (
              <Line
                {...lineProps}
                dataKey={aggregateSeries.name}
                stroke="currentColor"
                className="text-intent-warning-fill"
                isAnimationActive={shouldAnimate}
                animationDuration={ANIMATION_DURATION}
              />
            )}
          </LineChart>
        ) : (
          <></>
        )}
      </ChartWrapper>
    </div>
  );
};

export default KpiChart;
