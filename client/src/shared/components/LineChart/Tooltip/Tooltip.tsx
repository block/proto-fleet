import { ComponentType, type CSSProperties } from "react";
import clsx from "clsx";

import TooltipItem from "./TooltipItem";
import StatusCircle, { statuses, variants } from "@/shared/components/StatusCircle";
import { getDisplayValue } from "@/shared/utils/stringUtils";

type TooltipValue = string | number | null;
type TooltipDisplayValue = string | number;

type PayloadType = {
  name: string;
  payload: {
    datetime: number;
    [key: string]: TooltipValue;
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
  activeKeys?: string[];
  chartWidth?: number;
  coordinate?: { x: number; y: number };
  sortingFn?: (a: [string, TooltipDisplayValue], b: [string, TooltipDisplayValue]) => number;
  payload?: PayloadType[];
  units?: string;
  segmentsLabel?: string;
  tooltipWidth?: number;
  tooltipXOffset?: number;
  tooltipYOffset?: number;
  toolTipItemIcon?: ComponentType<{ itemKey: string }>;
}

const ChartTooltip = ({
  aggregateLabel,
  aggregateKey,
  colorMap,
  activeKeys = [],
  chartWidth = 0,
  coordinate = { x: 0, y: 0 },
  sortingFn,
  payload: payloads,
  units,
  segmentsLabel,
  tooltipWidth = 269,
  tooltipXOffset = 24,
  tooltipYOffset = 24,
  toolTipItemIcon,
}: ChartTooltipProps) => {
  // Use aggregateKey as fallback when no activeKeys provided
  const keysToShow = activeKeys && activeKeys.length > 0 ? activeKeys : aggregateKey ? [aggregateKey] : [];
  const showAggregate = aggregateKey ? keysToShow.includes(aggregateKey) : false;

  // filter payload to include only active keys
  const payload = payloads?.[0]?.payload;
  const filteredEntries = payload
    ? Object.entries(payload).filter((entry): entry is [string, TooltipDisplayValue] => {
        const [key, value] = entry;
        return keysToShow.includes(key) && (typeof value === "number" || typeof value === "string");
      })
    : [];

  // sort keys so they display in a consistent order
  const sortedEntries = sortingFn ? [...filteredEntries].sort(sortingFn) : filteredEntries;
  const aggregateValue = aggregateKey ? payload?.[aggregateKey] : undefined;
  const aggregateDisplayValue =
    aggregateValue !== null && aggregateValue !== undefined ? getDisplayValue(aggregateValue) : undefined;
  const shouldShowAggregate = Boolean(showAggregate && aggregateKey && aggregateDisplayValue !== undefined);
  const segmentEntries = sortedEntries.filter(([key]) => key !== aggregateKey);

  if (payload?.datetime === undefined || (!shouldShowAggregate && segmentEntries.length === 0)) {
    return null;
  }

  const wouldOverflowRight = chartWidth > 0 && coordinate.x + tooltipXOffset + tooltipWidth > chartWidth;
  const preferredTooltipTranslateX = wouldOverflowRight ? -tooltipWidth - tooltipXOffset : tooltipXOffset;
  const preferredTooltipLeft = coordinate.x + preferredTooltipTranslateX;
  const maxTooltipLeft = chartWidth > 0 ? Math.max(chartWidth - tooltipWidth, 0) : preferredTooltipLeft;
  const clampedTooltipLeft =
    chartWidth > 0 ? Math.min(Math.max(preferredTooltipLeft, 0), maxTooltipLeft) : preferredTooltipLeft;
  const tooltipTranslateX = clampedTooltipLeft - coordinate.x;
  const tooltipStyle: CSSProperties = {
    transform: `translate(${tooltipTranslateX}px, ${tooltipYOffset - coordinate.y}px)`,
  };

  return (
    <div
      className="pointer-events-none rounded-xl bg-surface-elevated-base/70 pt-6 pb-4 shadow-200 backdrop-blur-[7px]"
      style={tooltipStyle}
    >
      <div className="px-6" style={{ width: tooltipWidth + "px" }}>
        {shouldShowAggregate && aggregateKey && (
          <div
            className={clsx("flex space-x-2", {
              "pb-4": segmentEntries.length > 0,
            })}
          >
            <div>
              <div className="mb-1 text-200 text-text-primary-70">{aggregateLabel || aggregateKey}</div>
              <div className="inline-flex items-center gap-2 text-heading-100 text-text-primary">
                <StatusCircle width="w-2" status={statuses.warning} variant={variants.simple} />
                {aggregateDisplayValue} {units && <span>{units}</span>}
              </div>
            </div>
          </div>
        )}

        {segmentEntries.length > 0 && (
          <div>
            <div className="mb-1 text-200 text-text-primary-70">{segmentsLabel}</div>
            {segmentEntries.map(([key, value], idx) => {
              return (
                <TooltipItem
                  key={"tooltip-item-" + idx}
                  itemKey={key}
                  colorMap={colorMap}
                  units={units}
                  value={getDisplayValue(value)}
                  icon={toolTipItemIcon}
                />
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
};

export default ChartTooltip;
