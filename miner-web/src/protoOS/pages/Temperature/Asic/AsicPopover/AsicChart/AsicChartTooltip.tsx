import { useEffect, useMemo } from "react";

import AsicPopoverRow from "../AsicPopoverRow";
import { getDisplayValue } from "@/shared/utils/stringUtils";

type PayloadType = {
  payload: {
    datetime: number;
    temp_c?: number;
    hashrate_ghs?: number;
    value?: number;
  };
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
    [tooltipData],
  );

  return (
    <>
      {payload.datetime && (
        <div className="bg-surface-elevated-base/70 px-3 py-2 rounded-xl shadow-200 backdrop-blur-[7px] w-[180px]">
          {payload.temp_c !== undefined ? (
            <AsicPopoverRow
              label="Temperature"
              value={`${getDisplayValue(payload.temp_c)}º`}
              className="text-core-accent-fill"
            />
          ) : null}
          {payload.hashrate_ghs !== undefined ? (
            <AsicPopoverRow
              label="Hashrate"
              value={`${getDisplayValue(+payload.hashrate_ghs)} TH/s`}
              className="text-core-primary-fill"
            />
          ) : null}
        </div>
      )}
    </>
  );
};

export default AsicChartTooltip;
