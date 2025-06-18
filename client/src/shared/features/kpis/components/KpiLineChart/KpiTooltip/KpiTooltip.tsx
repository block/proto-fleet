import { useEffect } from "react";

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

export interface HashboardLocationStore {
  getSlotByHbSn: (serial: string) => number | null;
  getBayByHbSn: (serial: string) => number | null;
  getBayCount: () => number;
  getBaySlotIndexByHbSn: (serial: string) => number;
}

interface KpiTooltipProps {
  active?: boolean;
  coordinate?: { x: number; y: number };
  marginValue?: number;
  onHover: ({ payload, x, y }: TooltipData) => void;
  payload?: PayloadType[];
  tooltipData: TooltipData;
  units?: string;
  hashboardLocationStore?: HashboardLocationStore;
}

const KpiTooltip = ({
  active,
  coordinate = { x: 0, y: 0 },
  onHover,
  payload: payloads,
  tooltipData,
  units,
  hashboardLocationStore,
}: KpiTooltipProps) => {
  // Safely handle undefined hashboardLocationStore
  const getSlotByHbSn = hashboardLocationStore?.getSlotByHbSn || (() => null);
  const getBayByHbSn = hashboardLocationStore?.getBayByHbSn || (() => null);

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

  const entries = Object.entries(partials);
  const sorted = entries.sort((a, b) => {
    return (
      (getSlotByHbSn(a[0]) ?? entries.length) -
      (getSlotByHbSn(b[0]) ?? entries.length)
    );
  });

  const hasPartials = Object.keys(sorted).length > 0;

  return (
    <>
      {payload.datetime && (
        <div className="rounded-xl bg-surface-elevated-base/70 pt-6 pb-4 shadow-200 backdrop-blur-[7px]">
          <div className="w-[269px]">
            <div className="flex space-x-2 px-6">
              <div>
                <div className="mb-1 text-200 text-text-primary-70">
                  {payload.aggregateName}
                </div>
                <div className="text-heading-100 text-text-primary">
                  {getDisplayValue(total)}{" "}
                  {payload.units && <span>{payload.units}</span>}
                </div>
              </div>
            </div>

            {hasPartials ? <Divider className="mt-4 mb-6" /> : null}

            {sorted.map(([serial], idx) => {
              if (!hashboardLocationStore) return null;

              return (
                <KpiTooltipItem
                  key={idx}
                  currentPartial={idx}
                  totalPartials={Object.keys(partials).length}
                  serial={serial}
                  units={units}
                  bayDivider={
                    sorted[idx - 1] !== undefined &&
                    getBayByHbSn(serial) !== getBayByHbSn(sorted[idx - 1]?.[0])
                  }
                  value={getDisplayValue(payload[serial])}
                  hashboardLocationStore={hashboardLocationStore}
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
