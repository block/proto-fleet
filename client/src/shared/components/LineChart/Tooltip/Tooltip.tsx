import { ComponentType, useEffect, useRef } from "react";
import clsx from "clsx";

import TooltipItem from "./TooltipItem";
import StatusCircle, { statuses, variants } from "@/shared/components/StatusCircle";
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

interface ChartTooltipProps {
  aggregateLabel?: string;
  aggregateKey?: string;
  colorMap?: { [key: string]: string };
  active?: boolean;
  activeKeys?: string[];
  coordinate?: { x: number; y: number };
  sortingFn?: (a: [string, string | number], b: [string, string | number]) => number;
  marginValue?: number;
  onHover: ({ payload, x, y }: TooltipData) => void;
  payload?: PayloadType[];
  tooltipData: TooltipData;
  units?: string;
  segmentsLabel?: string;
  tooltipWidth?: number;
  toolTipItemIcon?: ComponentType<{ itemKey: string }>;
}

// Default value for lastUpdateRef flag to indicate no last update
const LAST_UPDATE_DEFAULT = {
  x: -1,
  y: -1,
  payloadLength: -1,
  active: false,
};

const ChartTooltip = ({
  aggregateLabel,
  aggregateKey,
  colorMap,
  active,
  activeKeys = [],
  coordinate = { x: 0, y: 0 },
  onHover,
  sortingFn,
  payload: payloads,
  tooltipData,
  units,
  segmentsLabel,
  tooltipWidth = 269,
  toolTipItemIcon,
}: ChartTooltipProps) => {
  const lastUpdateRef = useRef(LAST_UPDATE_DEFAULT);

  // Use aggregateKey as fallback when no activeKeys provided
  const keysToShow = activeKeys && activeKeys.length > 0 ? activeKeys : aggregateKey ? [aggregateKey] : [];

  const showAggregate = aggregateKey ? keysToShow.includes(aggregateKey) : false;

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
        // Extract only primitive data to avoid storing Recharts internal objects
        // which contain circular references that cause stack overflow during serialization
        const sanitizedPayloads = payloads.map((p) => ({
          name: p.name,
          payload: Object.fromEntries(
            Object.entries(p.payload).filter(([, value]) => typeof value === "number" || typeof value === "string"),
          ) as PayloadType["payload"],
        }));
        onHover({ payload: sanitizedPayloads, x: newX, y: newY });
      }
    } else if (!active && lastUpdateRef.current.active) {
      lastUpdateRef.current = LAST_UPDATE_DEFAULT;
      onHover({ payload: [], x: 0, y: 0 });
    }
  }, [active, coordinate.x, coordinate.y, payloads, onHover]);

  // filter payload to include only active keys
  const payload = tooltipData.payload[0]?.payload || {};
  const filteredPayload = Object.entries(payload).reduce(
    (acc, [key, value]) => {
      if (keysToShow.includes(key)) {
        acc[key] = value;
      }
      return acc;
    },
    {} as { [key: string]: string | number },
  );

  // sort keys so they display in a consistent order
  const filteredEntries = Object.entries(filteredPayload);
  const sortedKeys = sortingFn
    ? filteredEntries.sort(sortingFn).map(([key]) => key)
    : filteredEntries.map(([key]) => key);

  return (
    <>
      {payload.datetime && (
        <div className="rounded-xl bg-surface-elevated-base/70 pt-6 pb-4 shadow-200 backdrop-blur-[7px]">
          <div className="px-6" style={{ width: tooltipWidth + "px" }}>
            {showAggregate && aggregateKey && (
              <div
                className={clsx("flex space-x-2", {
                  "pb-4": sortedKeys.length > 0,
                })}
              >
                <div>
                  <div className="mb-1 text-200 text-text-primary-70">{aggregateLabel || aggregateKey}</div>
                  <div className="inline-flex items-center gap-2 text-heading-100 text-text-primary">
                    <StatusCircle width="w-2" status={statuses.warning} variant={variants.simple} />
                    {getDisplayValue(payload[aggregateKey])} {units && <span>{units}</span>}
                  </div>
                </div>
              </div>
            )}

            {sortedKeys.filter((key) => key !== aggregateKey).length > 0 && (
              <div>
                <div className="mb-1 text-200 text-text-primary-70">{segmentsLabel}</div>
                {sortedKeys
                  .filter((key) => key !== aggregateKey)
                  .map((key, idx) => {
                    return (
                      <TooltipItem
                        key={"tooltip-item-" + idx}
                        itemKey={key}
                        colorMap={colorMap}
                        units={units}
                        value={getDisplayValue(payload[key])}
                        icon={toolTipItemIcon}
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

export default ChartTooltip;
