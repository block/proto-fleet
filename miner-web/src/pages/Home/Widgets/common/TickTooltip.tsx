import { useEffect } from "react";

import { getStandardTime } from "common/utils/stringUtils";

import { getTickValue } from "components/Chart";

type PayloadType = {
  value: string | number;
  name: string;
  payload: { time: string };
};

type TooltipData = {
  payload: PayloadType[];
  x: number;
  y: number;
};

interface TickTooltipProps {
  active?: boolean;
  coordinate?: { x: number; y: number };
  marginValue?: number;
  onClick: ({ payload, x, y }: TooltipData) => void;
  payload?: PayloadType[];
  tooltipData: TooltipData;
  unit?: string;
}

const TickTooltip = ({
  active,
  coordinate = { x: 0, y: 0 },
  marginValue = 0,
  onClick,
  payload: payloads,
  tooltipData,
  unit,
}: TickTooltipProps) => {
  useEffect(() => {
    if (
      active &&
      payloads &&
      payloads.length > 0 &&
      coordinate.x !== tooltipData.x
    ) {
      onClick({ payload: payloads, x: coordinate.x, y: coordinate.y });
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
            {`${getTickValue(+payload.value, marginValue)} ${unit}`}
          </div>
        </div>
      ))}
    </div>
  );
};

export default TickTooltip;
