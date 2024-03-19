import { useCallback, useEffect, useRef, useState } from "react";

import { useClickOutside } from "common/hooks/useClickOutside";
import { deepClone } from "common/utils/utility";

import {
  Bar,
  BarChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import { chartData, marginValue } from "./constants";
import PowerUsageXAxisTick from "./PowerUsageAxis/PowerUsageXAxisTick";
import PowerUsageYAxisTick from "./PowerUsageAxis/PowerUsageYAxisTick";
import PowerUsageBar from "./PowerUsageBar";
import PowerUsageTooltip, { TooltipData } from "./PowerUsageTooltip";

interface chartDataProps {
  time?: string;
  value: number;
}

const PowerUsageChart = () => {
  const [tooltipData, setTooltipData] = useState<TooltipData>({
    x: 0,
    payload: [],
  });
  const [isTooltipActive, setTooltipActive] = useState(false);
  const tooltipRef = useRef(null);

  // TODO: get chart data from API when available
  const [chartDataPadded, setChartDataPadded] = useState<chartDataProps[] | []>(
    []
  );

  useEffect(() => {
    const newData = deepClone(chartData);
    setChartDataPadded(
      newData.map((data: chartDataProps) => {
        data.value += marginValue;
        return data;
      })
    );
  }, []);

  const onClickOutside = useCallback(() => {
    setTooltipActive(false);
    setTooltipData({ x: 0, payload: [] });
  }, []);

  useClickOutside({ ref: tooltipRef, onClickOutside });

  const maxValue = Math.max(...chartData.map((data) => data.value));

  return (
    <div ref={tooltipRef} className="flex w-full h-full">
      <ResponsiveContainer width="100%" height="100%">
        <BarChart
          data={chartDataPadded}
          margin={{
            top: 16,
            right: 0,
            left: -34,
            bottom: 0,
          }}
          onClick={() => setTooltipActive(true)}
        >
          <XAxis
            dataKey="time"
            axisLine={false}
            tickLine={false}
            interval={0}
            tick={PowerUsageXAxisTick}
          />
          <YAxis
            axisLine={false}
            tickLine={false}
            interval={0}
            tick={PowerUsageYAxisTick}
            scale="linear"
            domain={[0, maxValue + 1]}
            tickMargin={6}
            padding={{ top: -26, bottom: 25 }}
          />
          <Tooltip
            active={isTooltipActive}
            cursor={{ fill: "#fff" }}
            position={{ y: -75, x: tooltipData.x - 50 }}
            content={
              <PowerUsageTooltip
                onClick={setTooltipData}
                tooltipData={tooltipData}
              />
            }
            trigger="click"
          />
          <Bar
            dataKey="value"
            barSize={16}
            radius={[0, 0, 4, 4]}
            shape={<PowerUsageBar />}
            activeBar={<PowerUsageBar active={!!tooltipData.payload.length} />}
            className="hover:cursor-pointer"
          />
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
};

export default PowerUsageChart;
