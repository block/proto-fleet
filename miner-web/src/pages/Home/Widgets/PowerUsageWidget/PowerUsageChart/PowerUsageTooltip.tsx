import { useEffect } from "react";

import { getStandardTime } from "common/utils/stringUtils";

import { marginValue } from "./constants";

type PayloadType = {
  value: string | number;
  name: string;
  payload: { time: string };
};

export type TooltipData = {
  payload: PayloadType[];
  x: number;
};

interface CustomTooltipProps {
  active?: boolean;
  coordinate?: { x: number };
  onClick: ({ payload, x }: TooltipData) => void;
  payload?: PayloadType[];
  tooltipData: TooltipData;
}

const PowerUsageTooltip = ({
  active,
  coordinate = { x: 0 },
  onClick,
  payload: payloads,
  tooltipData,
}: CustomTooltipProps) => {
  useEffect(() => {
    if (
      active &&
      payloads &&
      payloads.length > 0 &&
      coordinate.x !== tooltipData.x
    ) {
      onClick({ payload: payloads, x: coordinate.x });
    }
  }, [active, coordinate, onClick, payloads, tooltipData]);

  return (
    <div className="bg-surface-base/70 px-3 py-2 rounded-xl shadow-200 backdrop-blur-[7px]">
      {tooltipData.payload.map((payload: PayloadType) => (
        <div key={payload.name} className="w-[74px]">
          <div className="text-200 mb-1 text-text-primary/70">
            {getStandardTime(payload.payload.time)}
          </div>
          <div className="text-heading-100 text-text-primary">
            {`${Math.round((+payload.value - marginValue) * 100) / 100} kW`}
          </div>
        </div>
      ))}
    </div>
  );
};

export default PowerUsageTooltip;
