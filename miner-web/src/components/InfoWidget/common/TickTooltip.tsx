import { useEffect } from "react";

import { getDisplayValue } from "common/utils/stringUtils";

type PayloadType = {
  value: string | number;
  name: string;
  payload: { datetime: number };
};

export type TooltipData = {
  payload: PayloadType[];
  x: number;
  y: number;
};

interface TickTooltipProps {
  active?: boolean;
  coordinate?: { x: number; y: number };
  onHover: ({ payload, x, y }: TooltipData) => void;
  payload?: PayloadType[];
  tooltipData: TooltipData;
  unit?: string;
}

const TickTooltip = ({
  active,
  coordinate = { x: 0, y: 0 },
  onHover,
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
      onHover({ payload: payloads, x: coordinate.x, y: coordinate.y });
    } else if (!active && tooltipData.payload.length > 0) {
      onHover({ payload: [], x: 0, y: 0 });
    }
  }, [active, coordinate, onHover, payloads, tooltipData]);

  return (
    <div className="bg-surface-elevated-base/70 px-3 py-2 rounded-xl shadow-200 backdrop-blur-[7px]">
      {tooltipData.payload.map((payload: PayloadType) => (
        <div key={payload.name}>
          <div className="text-heading-100 text-text-primary">
            {`${getDisplayValue(payload.value)} ${unit}`}
          </div>
        </div>
      ))}
    </div>
  );
};

export default TickTooltip;
