import { ReactNode, useEffect, useMemo, useState } from "react";
import { Line, LineChart, ReferenceDot, Tooltip, XAxis, YAxis } from "recharts";

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
  LineDot,
  TimeXAxisTick,
  xAxisProps,
  yAxisProps,
} from "@/shared/components/Chart";
import useCssVariable from "@/shared/hooks/useCssVariable";
import useMeasure from "@/shared/hooks/useMeasure";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const ANIMATION_DURATION = 1500;

export interface KpiChartProps {
  series: TimeSeriesWithSerial[];
  units?: string;
  aggregateSeries: TimeSeries;
  highestValue?: string | number;
  hashboardLocationStore?: HashboardLocationStore;
  getHashboardColorMap?: (series: TimeSeriesWithSerial[]) => {
    [key: string]: {
      line: string;
      text: string;
    };
  };
}

const KpiChart = ({
  series,
  aggregateSeries,
  highestValue,
  units,
  hashboardLocationStore,
  getHashboardColorMap,
}: KpiChartProps) => {
  const [chartRef, chartRect] = useMeasure<HTMLDivElement>();
  const [tooltipData, setTooltipData] = useState<TooltipData>({
    x: 0,
    y: 0,
    payload: [],
  });

  const corePrimary5 = useCssVariable("--color-core-primary-5");
  const corePrimary20 = useCssVariable("--color-core-primary-20");

  const [shouldAnimate, setShouldAnimate] = useState(true);
  const { isDesktop, isTablet, isLaptop, isPhone } = useWindowDimensions();
  const [chartData, setChartData] = useState<ChartData[] | null>(null);
  const [maxDomain, setMaxDomain] = useState<number>(0);

  // Get the hashboard color map
  const [hbColorMap, setHbColorMap] = useState<{
    [key: string]: {
      line: string;
      text: string;
    };
  } | null>(null);

  useEffect(() => {
    if (!getHashboardColorMap) return;

    setHbColorMap(getHashboardColorMap(series));
  }, [series, getHashboardColorMap]);

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
      Math.max(...chartData.map((data) => +(data[aggregateSeries.name] || 0))) *
        1.1;

    const nearestTen = Math.round(max / 10) * 10;
    setMaxDomain(nearestTen + (max < 10 ? 5 : 20));
  }, [chartData, highestValue, aggregateSeries.name]);

  const yAxisTickCount = maxDomain / 5 + 10;

  const gridDots = useMemo(() => {
    if (!chartData?.length || !chartRect.width || !chartRect.height)
      return null;

    const spacing = 20;
    const verticalLines: number[] = [];
    const horizontalLines: number[] = [];
    const minX = chartData[0]?.datetime;
    const maxX = chartData[chartData.length - 1]?.datetime;

    if (!minX || !maxX) return null;

    for (let i = 1; i <= Math.floor(chartRect.width / spacing); i++) {
      const pixelX = i * spacing;

      if (pixelX > 40 && pixelX <= chartRect.width - 20) {
        const xPercentage = (pixelX - 40) / (chartRect.width - 60);
        const dataIndex = Math.min(
          Math.floor(xPercentage * chartData.length),
          chartData.length - 1,
        );
        if (dataIndex >= 0) {
          verticalLines.push(dataIndex);
        }
      }
    }

    for (let j = 1; j <= Math.floor(chartRect.height / spacing); j++) {
      const pixelY = j * spacing;

      if (pixelY <= chartRect.height - 30) {
        const yValue = (1 - pixelY / (chartRect.height - 30)) * maxDomain;
        if (yValue >= 0 && yValue <= maxDomain) {
          horizontalLines.push(yValue);
        }
      }
    }

    const dots: ReactNode[] = [];

    verticalLines.forEach((dataIndex, iX) => {
      const x = chartData[dataIndex]?.datetime;
      if (!x) return;

      horizontalLines.forEach((y, iY) => {
        dots.push(
          <ReferenceDot
            key={`dot-${iX}-${iY}`}
            x={x}
            y={y}
            r={1}
            fill={corePrimary20}
            stroke="none"
            isFront={false}
          />,
        );
      });
    });

    return dots;
  }, [chartData, chartRect.width, chartRect.height, maxDomain, corePrimary20]);

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
            {gridDots}

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
                  hashboardLocationStore={hashboardLocationStore}
                />
              }
              cursor={<LineCursor />}
              isAnimationActive={false}
            />
            {!!tooltipData.payload.length && !shouldAnimate && hbColorMap && (
              <>
                {series.map((seriesItem, index) => {
                  if (seriesItem.data.length) {
                    return (
                      <Line
                        {...lineProps}
                        dataKey={seriesItem.serial}
                        key={index}
                        isAnimationActive={false}
                        stroke={`var(${hbColorMap[seriesItem.serial]?.line})`}
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
              isAnimationActive={shouldAnimate}
              animationDuration={ANIMATION_DURATION}
            />
          </LineChart>
        ) : (
          <></>
        )}
      </ChartWrapper>
    </div>
  );
};

export default KpiChart;
