import { useEffect, useRef } from "react";
import clsx from "clsx";

import KpiTooltipItem from "./KpiTooltipItem";
import StatusCircle, {
  statuses,
  variants,
} from "@/shared/components/StatusCircle";
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
  aggregateLabel?: string;
  active?: boolean;
  activeSeries?: string[];
  coordinate?: { x: number; y: number };
  marginValue?: number;
  onHover: ({ payload, x, y }: TooltipData) => void;
  payload?: PayloadType[];
  tooltipData: TooltipData;
  units?: string;
  hashboardLocationStore?: HashboardLocationStore;
  showAggregate?: boolean;
  tooltipWidth?: number;
}

// Default value for lastUpdateRef flag to indicate no last update
const LAST_UPDATE_DEFAULT = {
  x: -1,
  y: -1,
  payloadLength: -1,
  active: false,
};

const KpiTooltip = ({
  aggregateLabel,
  active,
  activeSeries = [],
  coordinate = { x: 0, y: 0 },
  onHover,
  payload: payloads,
  tooltipData,
  units,
  hashboardLocationStore,
  showAggregate,
  tooltipWidth = 269,
}: KpiTooltipProps) => {
  // Safely handle undefined hashboardLocationStore
  const getSlotByHbSn = hashboardLocationStore?.getSlotByHbSn || (() => null);

  const lastUpdateRef = useRef(LAST_UPDATE_DEFAULT);

  useEffect(() => {
    if (active && payloads && payloads.length > 0) {
      // Only update if the data has actually changed
      const newX = coordinate.x;
      const newY = coordinate.y;
      const payloadLength = payloads.length;

      if (
        lastUpdateRef.current.x !== newX ||
        lastUpdateRef.current.y !== newY ||
        lastUpdateRef.current.payloadLength !== payloadLength ||
        lastUpdateRef.current.active !== active
      ) {
        lastUpdateRef.current = { x: newX, y: newY, payloadLength, active };
        onHover({ payload: payloads, x: newX, y: newY });
      }
    } else if (!active && lastUpdateRef.current.active) {
      lastUpdateRef.current = LAST_UPDATE_DEFAULT;
      onHover({ payload: [], x: 0, y: 0 });
    }
  }, [active, coordinate.x, coordinate.y, payloads, onHover]);

  const payload = tooltipData.payload[0]?.payload || {};
  const hashboards = omit(payload, [
    "datetime",
    "aggregateName",
    "units",
    payload.aggregateName,
  ]);

  const entries = Object.entries(hashboards);
  const sortedHashboards = entries.sort((a, b) => {
    return (
      (getSlotByHbSn(a[0]) ?? entries.length) -
      (getSlotByHbSn(b[0]) ?? entries.length)
    );
  });

  const totalSlots = sortedHashboards.length
    ? (getSlotByHbSn(sortedHashboards[sortedHashboards.length - 1][0]) ?? 0)
    : 0;

  const filteredPayload = Object.entries(payload).reduce(
    (acc, [key, value]) => {
      if (activeSeries?.includes(key)) {
        acc[key] = value;
      }
      return acc;
    },
    {} as { [key: string]: string | number },
  );

  const sortedEntries = Object.entries(filteredPayload);
  const sortedFilteredHashboards = sortedEntries.sort((a, b) => {
    return (
      (getSlotByHbSn(a[0]) ?? sortedEntries.length) -
      (getSlotByHbSn(b[0]) ?? sortedEntries.length)
    );
  });

  return (
    <>
      {payload.datetime && (
        <div className="rounded-xl bg-surface-elevated-base/70 pt-6 pb-4 shadow-200 backdrop-blur-[7px]">
          <div className="px-6" style={{ width: tooltipWidth + "px" }}>
            {showAggregate && (
              <div
                className={clsx("flex space-x-2", {
                  "pb-4": sortedFilteredHashboards.length > 0,
                })}
              >
                <div>
                  <div className="mb-1 text-200 text-text-primary-70">
                    {aggregateLabel || payload.aggregateName}
                  </div>
                  <div className="inline-flex items-center gap-2 text-heading-100 text-text-primary">
                    <StatusCircle
                      width="w-2"
                      status={statuses.warning}
                      variant={variants.simple}
                    />
                    {getDisplayValue(payload[payload.aggregateName])}{" "}
                    {payload.units && <span>{payload.units}</span>}
                  </div>
                </div>
              </div>
            )}

            {sortedFilteredHashboards.length > 0 && (
              <div>
                <div className="mb-1 text-200 text-text-primary-70">
                  Hashboards
                </div>
                {sortedFilteredHashboards.map(([serial], idx) => {
                  if (!hashboardLocationStore) return null;

                  return (
                    <KpiTooltipItem
                      key={idx}
                      currentPartial={idx}
                      totalSlots={totalSlots}
                      serial={serial}
                      units={units}
                      value={getDisplayValue(payload[serial])}
                      hashboardLocationStore={hashboardLocationStore}
                    />
                  );
                })}
              </div>
            )}
          </div>
        </div>
      )}
    </>
  );
};

export default KpiTooltip;
