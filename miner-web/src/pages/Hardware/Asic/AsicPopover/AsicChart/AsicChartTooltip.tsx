import { useEffect, useMemo } from "react";

import { getStandardTime } from "common/utils/stringUtils";

import { getTickValue } from "components/Chart";

import AsicChartTooltipItem from "./AsicChartTooltipItem";

type PayloadType = {
  payload: { time: string; temp_c: number; hashrate_ghs: number };
};

export type TooltipData = {
  payload: PayloadType[];
  x: number;
  y: number;
};

interface AsicChartTooltipProps {
  active?: boolean;
  coordinate?: { x: number; y: number };
  onHover?: ({ payload, x, y }: TooltipData) => void;
  payload?: PayloadType[];
  tooltipData: TooltipData;
}

const AsicChartTooltip = ({
  active,
  coordinate = { x: 0, y: 0 },
  onHover,
  payload: payloads,
  tooltipData,
}: AsicChartTooltipProps) => {
  useEffect(() => {
    if (
      active &&
      payloads &&
      payloads.length > 0 &&
      coordinate.x !== tooltipData.x
    ) {
      onHover?.({ payload: payloads, x: coordinate.x, y: coordinate.y });
    } else if (!active && tooltipData.payload.length > 0) {
      onHover?.({ payload: [], x: 0, y: 0 });
    }
  }, [active, coordinate, onHover, payloads, tooltipData]);

  const payload = useMemo(
    () => tooltipData.payload[0]?.payload || {},
    [tooltipData]
  );

  return (
    <>
      {payload.time && (
        <div className="bg-surface-base/70 px-3 py-2 rounded-xl shadow-200 backdrop-blur-[7px] w-[180px]">
          <div className="flex justify-end mb-2">
            <div className="text-200 text-text-primary/70 rounded-lg px-2 py-[2px] bg-surface-5 h-fit w-fit">
              {getStandardTime(payload.time)}
            </div>
          </div>
          <AsicChartTooltipItem
            label="Temperature"
            value={`${getTickValue(payload.temp_c)}º`}
            colorClassName="bg-core-accent-fill"
          />
          <AsicChartTooltipItem
            label="Hashrate"
            value={`${getTickValue(+payload.hashrate_ghs)} TH/s`}
            colorClassName="bg-core-primary-fill"
          />
        </div>
      )}
    </>
  );
};

export default AsicChartTooltip;
