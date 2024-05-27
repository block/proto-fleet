import { useEffect, useMemo } from "react";

import Divider from "components/Divider";

import HashrateTooltipItem from "./HashrateTooltipItem";

type PayloadType = {
  name: string;
  payload: {
    avgHashrate: string | number;
    hashrate1?: string | number;
    hashrate2?: string | number;
    hashrate3?: string | number;
    time: string;
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
    [tooltipData]
  );

  return (
    <>
      {payload.time && (
        <div className="bg-surface-base/70 pt-6 pb-4 rounded-xl shadow-200 backdrop-blur-[7px]">
          <div className="w-[269px]">
            <div className="flex space-x-2 px-6">
              <div className="w-1 h-3 bg-core-accent-fill rounded-sm mt-1" />
              <div>
                <div className="text-200 mb-1 text-text-primary/70">
                  Total Hashrate
                </div>
                <div className="text-heading-100 text-text-primary">
                  {payload.avgHashrate} TH/s
                </div>
              </div>
            </div>
            {payload.hashrate1 || payload.hashrate2 || payload.hashrate3 ? (
              <Divider className="mt-4 mb-6" />
            ) : null}
            <HashrateTooltipItem
              colorClassName="bg-intent-info-fill"
              label="Hashboard 1"
              value={payload.hashrate1}
            />
            <HashrateTooltipItem
              colorClassName="bg-intent-success-fill"
              label="Hashboard 2"
              value={payload.hashrate2}
            />
            <HashrateTooltipItem
              colorClassName="bg-core-indigo"
              label="Hashboard 3"
              value={payload.hashrate3}
            />
          </div>
        </div>
      )}
    </>
  );
};

export default HashrateTooltip;
