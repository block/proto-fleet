import { useEffect, useState } from "react";

import LineChart from "./LineChart";
import { Line } from "./LineChart/types";

const time = ["04:00", "08:00", "12:00", "16:00", "20:00", "24:00"];

const boards = {
  board1: "Board 1",
  board2: "Board 2",
  board3: "Board 3",
  color: "#c6c6c6",
  getRandomValue: () => Math.floor(Math.random() * 21),
  strokeWidth: 2,
};

const getChartData = (avgLabel: string, timeIndex: number) => {
  const board1 = boards.getRandomValue();
  const board2 = boards.getRandomValue();
  const board3 = boards.getRandomValue();
  return {
    time: time[timeIndex],
    [boards.board1]: board1,
    [boards.board2]: board2,
    [boards.board3]: board3,
    [avgLabel]: board1 + board2 + board3,
  };
};

const pageData = (avgLabel: string) =>
  [...Array(6)].map((_, i) => getChartData(avgLabel, i));

const height = 300;
const width = 500;

const efficiency = {
  color: "#F46E38",
  avgData: pageData("Efficiency").map((d) => ({
    time: d.time,
    Efficiency: d.Efficiency,
  })),
  data: pageData("Efficiency"),
  label: "Efficiency",
  strokeWidth: 3,
  unit: "J/TH",
};

const hashrate = {
  color: "#008096",
  avgData: pageData("Hashrate").map((d) => ({
    time: d.time,
    Hashrate: d.Hashrate,
  })),
  data: pageData("Hashrate"),
  label: "Hashrate",
  strokeWidth: 3,
  unit: "TH/s",
};

const newDataInterval = 3000;

interface IntervalProps {
  data: any;
  dataPoints: Record<string, number | string>;
  setData: (data: any) => void;
  avgLabel: string;
}

const getTimeIndex = (data: any) =>
  data[data.length - 1].time === time[5]
    ? 0
    : time.indexOf(data[data.length - 1].time) + 1;

const useInterval = ({
  data,
  dataPoints,
  setData,
  avgLabel,
}: IntervalProps) => {
  useEffect(() => {
    const intervalId = setInterval(() => {
      const newData = [...data];
      newData.shift();
      setData(
        [...newData].concat({
          ...dataPoints,
        })
      );
    }, newDataInterval);

    return () => clearInterval(intervalId);
  }, [avgLabel, data, dataPoints, setData]);
};

interface ChartProps {
  avgLabel: string;
  chartData: any;
  lines: Line[];
  height?: number;
  unit: string;
  width?: number;
}

const Chart = ({
  avgLabel,
  chartData,
  lines,
  height,
  unit,
  width,
}: ChartProps) => {
  return (
    <div style={{ height, width }}>
      <LineChart
        data={chartData}
        lines={lines}
        title={`${chartData[chartData.length - 1][avgLabel]} ${unit}`}
        height={height}
        width={width}
      />
    </div>
  );
};

interface TrendChartProps extends Omit<ChartProps, "lines"> {
  avgColor: string;
  avgStrokeWidth: number;
}

const TrendChart = ({
  avgLabel,
  avgColor,
  avgStrokeWidth,
  chartData,
  unit,
  width,
}: TrendChartProps) => {
  const [data, setData] = useState(chartData);

  useInterval({
    data,
    dataPoints: getChartData(avgLabel, getTimeIndex(data)),
    setData,
    avgLabel,
  });

  return (
    <Chart
      avgLabel={avgLabel}
      chartData={data}
      lines={[
        {
          dataKey: avgLabel,
          stroke: avgColor,
          strokeWidth: avgStrokeWidth,
        },
        {
          dataKey: boards.board1,
          stroke: boards.color,
          strokeWidth: boards.strokeWidth,
        },
        {
          dataKey: boards.board2,
          stroke: boards.color,
          strokeWidth: boards.strokeWidth,
        },
        {
          dataKey: boards.board3,
          stroke: boards.color,
          strokeWidth: boards.strokeWidth,
        },
      ]}
      height={height}
      unit={unit}
      width={width}
    />
  );
};

const AvgChart = ({
  avgLabel,
  avgColor,
  avgStrokeWidth,
  chartData,
  unit,
  height,
  width,
}: TrendChartProps) => {
  const [data, setData] = useState(chartData);

  useInterval({
    data,
    dataPoints: {
      [avgLabel]: Math.floor(Math.random() * (60 - 20 + 1) + 20),
      time: time[getTimeIndex(data)],
    },
    setData,
    avgLabel,
  });

  return (
    <Chart
      avgLabel={avgLabel}
      chartData={data}
      lines={[
        {
          dataKey: avgLabel,
          stroke: avgColor,
          strokeWidth: avgStrokeWidth,
        },
      ]}
      height={height}
      unit={unit}
      width={width}
    />
  );
};

interface SizeProps extends Pick<ChartProps, "height" | "width"> {}

export const Efficiency = ({ width }: SizeProps) => {
  return (
    <TrendChart
      unit={efficiency.unit}
      avgLabel={efficiency.label}
      avgColor={efficiency.color}
      avgStrokeWidth={efficiency.strokeWidth}
      chartData={efficiency.data}
      width={width}
    />
  );
};

export const Hashrate = ({ width }: SizeProps) => {
  return (
    <TrendChart
      unit={hashrate.unit}
      avgLabel={hashrate.label}
      avgColor={hashrate.color}
      avgStrokeWidth={hashrate.strokeWidth}
      chartData={hashrate.data}
      width={width}
    />
  );
};

Efficiency.args = Hashrate.args = {
  width,
};

export const AvgEfficiency = ({ height, width }: SizeProps) => {
  return (
    <AvgChart
      unit={efficiency.unit}
      avgLabel={efficiency.label}
      avgColor={efficiency.color}
      avgStrokeWidth={efficiency.strokeWidth}
      chartData={efficiency.avgData}
      height={height}
      width={width}
    />
  );
};

export const AvgHashrate = ({ height, width }: SizeProps) => {
  return (
    <AvgChart
      unit={hashrate.unit}
      avgLabel={hashrate.label}
      avgColor={hashrate.color}
      avgStrokeWidth={hashrate.strokeWidth}
      chartData={hashrate.avgData}
      height={height}
      width={width}
    />
  );
};

AvgEfficiency.args = AvgHashrate.args = {
  height,
  width,
};

export default {
  component: Efficiency,
  title: "Charts",
};
