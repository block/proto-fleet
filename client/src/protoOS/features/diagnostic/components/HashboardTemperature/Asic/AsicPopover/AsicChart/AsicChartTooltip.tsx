import { useEffect, useMemo } from "react";

import AsicPopoverRow from "../AsicPopoverRow";
import { convertAndFormatTemperature } from "@/protoOS/features/diagnostic/components/HashboardTemperature/Asic/AsicPopover/utility";
import { useTemperatureUnit } from "@/protoOS/store";
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
  const temperatureUnit = useTemperatureUnit();

  useEffect(() => {
    if (active && payloads && payloads.length > 0 && coordinate.x !== tooltipData.x) {
      onHover?.({ payload: payloads, x: coordinate.x, y: coordinate.y });
    } else if (!active && tooltipData.payload.length > 0) {
      onHover?.({ payload: [], x: 0, y: 0 });
    }
  }, [active, coordinate, onHover, payloads, tooltipData]);

  const payload = useMemo(() => tooltipData.payload[0]?.payload || {}, [tooltipData]);

  return (
    <>
      {payload.datetime && (
        <div className="w-[180px] rounded-xl bg-surface-elevated-base/70 px-3 py-2 shadow-200 backdrop-blur-[7px]">
          {payload.temp_c !== undefined ? (
            <AsicPopoverRow
              label="Temperature"
              value={`${convertAndFormatTemperature(payload.temp_c, temperatureUnit)}`}
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
