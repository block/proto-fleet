import { useEffect, useMemo } from "react";

import HashrateTooltipItem from "./HashrateTooltipItem";
import Divider from "@/shared/components/Divider";
import { getDisplayValue } from "@/shared/utils/stringUtils";

type PayloadType = {
  name: string;
  payload: {
    datetime: number;
    hashrate1?: string | number;
    hashrate2?: string | number;
    hashrate3?: string | number;
    totalHashrate: string | number;
  };
};

export type TooltipData = {
  payload: PayloadType[];
  x: number;
  y: number;
};

interface HashrateTooltipProps {
  active?: boolean;
  coordinate?: { x: number; y: number };
  marginValue?: number;
  onHover: ({ payload, x, y }: TooltipData) => void;
  payload?: PayloadType[];
  tooltipData: TooltipData;
}

const HashrateTooltip = ({
  active,
  coordinate = { x: 0, y: 0 },
  onHover,
  payload: payloads,
  tooltipData,
}: HashrateTooltipProps) => {
  useEffect(() => {
    const x = coordinate.x < 310 ? coordinate.x + 310 : coordinate.x;
    if (active && payloads && payloads.length > 0 && x !== tooltipData.x) {
      onHover({ payload: payloads, x, y: coordinate.y });
    } else if (!active && tooltipData.payload.length > 0) {
      onHover({ payload: [], x: 0, y: 0 });
    }
  }, [active, coordinate, onHover, payloads, tooltipData]);

  const payload = useMemo(
    () => tooltipData.payload[0]?.payload || {},
    [tooltipData],
  );

  const hasHashrate1 = payload.hashrate1 !== undefined;
  const hasHashrate2 = payload.hashrate2 !== undefined;
  const hasHashrate3 = payload.hashrate3 !== undefined;
  const hasHashrate = hasHashrate1 || hasHashrate2 || hasHashrate3;

  return (
    <>
      {payload.datetime && (
        <div className="bg-surface-elevated-base/70 pt-6 pb-4 rounded-xl shadow-200 backdrop-blur-[7px]">
          <div className="w-[269px]">
            <div className="flex space-x-2 px-6">
              <div className="w-1 h-3 bg-core-accent-fill rounded-xs mt-1" />
              <div>
                <div className="text-200 mb-1 text-text-primary-70">
                  Total Hashrate
                </div>
                <div className="text-heading-100 text-text-primary">
                  {getDisplayValue(payload.totalHashrate)} TH/s
                </div>
              </div>
            </div>
            {hasHashrate ? <Divider className="mt-4 mb-6" /> : null}
            {hasHashrate1 ? (
              <HashrateTooltipItem
                colorClassName="bg-intent-info-fill"
                label="Hashboard 1"
                value={getDisplayValue(payload.hashrate1)}
              />
            ) : null}
            {hasHashrate2 ? (
              <HashrateTooltipItem
                colorClassName="bg-intent-success-fill"
                label="Hashboard 2"
                value={getDisplayValue(payload.hashrate2)}
              />
            ) : null}
            {hasHashrate3 ? (
              <HashrateTooltipItem
                colorClassName="bg-core-indigo-fill"
                label="Hashboard 3"
                value={getDisplayValue(payload.hashrate3)}
              />
            ) : null}
          </div>
        </div>
      )}
    </>
  );
};

export default HashrateTooltip;
