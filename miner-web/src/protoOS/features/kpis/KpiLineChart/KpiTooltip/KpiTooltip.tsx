import { useEffect } from "react";

import { lineColors } from "../constants";
import KpiTooltipItem from "./KpiTooltipItem";
import Divider from "@/shared/components/Divider";
import { omit } from "@/shared/utils/object";
import { getDisplayValue } from "@/shared/utils/stringUtils";

type PayloadType = {
  name: string;
  payload: {
    datetime: number;
    [key: string]: string | number;
  };
};

export type TooltipData = {
  payload: PayloadType[];
  x: number;
  y: number;
};

interface KpiTooltipProps {
  active?: boolean;
  coordinate?: { x: number; y: number };
  marginValue?: number;
  onHover: ({ payload, x, y }: TooltipData) => void;
  payload?: PayloadType[];
  tooltipData: TooltipData;
  units?: string;
}

const KpiTooltip = ({
  active,
  coordinate = { x: 0, y: 0 },
  onHover,
  payload: payloads,
  tooltipData,
  units,
}: KpiTooltipProps) => {
  useEffect(() => {
    const x = coordinate.x < 310 ? coordinate.x + 310 : coordinate.x;
    if (active && payloads && payloads.length > 0 && x !== tooltipData.x) {
      onHover({ payload: payloads, x, y: coordinate.y });
    } else if (!active && tooltipData.payload.length > 0) {
      onHover({ payload: [], x: 0, y: 0 });
    }
  }, [active, coordinate, onHover, payloads, tooltipData]);

  const payload = tooltipData.payload[0]?.payload || {};
  const total = payload[payload.aggregateName];
  const partials = omit(payload, [
    "datetime",
    "aggregateName",
    "units",
    payload.aggregateName,
  ]);

  const hasPartials = Object.keys(partials).length > 0;

  return (
    <>
      {payload.datetime && (
        <div className="bg-surface-elevated-base/70 pt-6 pb-4 rounded-xl shadow-200 backdrop-blur-[7px]">
          <div className="w-[269px]">
            <div className="flex space-x-2 px-6">
              <div className="w-1 h-3 bg-core-accent-fill rounded-xs mt-1" />
              <div>
                <div className="text-200 mb-1 text-text-primary-70">
                  {payload.aggregateName}
                </div>
                <div className="text-heading-100 text-text-primary">
                  {getDisplayValue(total)}{" "}
                  {payload.units && <span>{payload.units}</span>}
                </div>
              </div>
            </div>

            {hasPartials ? <Divider className="mt-4 mb-6" /> : null}

            {Object.keys(partials).map((name, idx) => {
              return (
                <KpiTooltipItem
                  key={idx}
                  color={lineColors[idx % lineColors.length]}
                  label={name}
                  units={units}
                  value={getDisplayValue(payload[name])}
                />
              );
            })}
          </div>
        </div>
      )}
    </>
  );
};

export default KpiTooltip;
