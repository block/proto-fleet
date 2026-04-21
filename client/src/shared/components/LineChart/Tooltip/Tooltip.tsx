import { ComponentType, type CSSProperties } from "react";
import clsx from "clsx";

import type { ChartData } from "../types";
import TooltipItem from "./TooltipItem";
import StatusCircle, { statuses, variants } from "@/shared/components/StatusCircle";
import { getDisplayValue } from "@/shared/utils/stringUtils";

type TooltipValue = string | number | null;
type TooltipDisplayValue = string | number;
const AGGREGATE_TOOLTIP_STATUS_CIRCLE_TEST_ID = "aggregate-tooltip-status-circle";

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

function isDisplayableValue(value: TooltipValue | undefined): value is TooltipDisplayValue {
  return typeof value === "number" || typeof value === "string";
}

function hasDisplayableValue(point: ChartData, keysToShow: string[]): boolean {
  return keysToShow.some((key) => isDisplayableValue(point[key]));
}

function findNearestDisplayablePoint(
  chartData: ChartData[],
  currentDatetime: number,
  keysToShow: string[],
): ChartData | undefined {
  let firstDisplayable: ChartData | undefined;
  let lastDisplayable: ChartData | undefined;
  let nearest: ChartData | undefined;
  let minDistance = Infinity;

  for (const point of chartData) {
    if (!hasDisplayableValue(point, keysToShow)) continue;

    if (!firstDisplayable) firstDisplayable = point;
    lastDisplayable = point;

    const distance = Math.abs(point.datetime - currentDatetime);
    if (distance < minDistance) {
      minDistance = distance;
      nearest = point;
    }
  }

  if (
    !firstDisplayable ||
    !lastDisplayable ||
    currentDatetime < firstDisplayable.datetime ||
    currentDatetime > lastDisplayable.datetime
  ) {
    return undefined;
  }

  return nearest;
}

interface ChartTooltipProps {
  aggregateLabel?: string;
  aggregateKey?: string;
  colorMap?: { [key: string]: string };
  activeKeys?: string[];
  chartData?: ChartData[] | null;
  chartWidth?: number;
  connectNulls?: boolean;
  coordinate?: { x: number; y: number };
  label?: number | string;
  sortingFn?: (a: [string, TooltipDisplayValue], b: [string, TooltipDisplayValue]) => number;
  payload?: PayloadType[];
  units?: string;
  segmentsLabel?: string;
  tooltipWidth?: number;
  tooltipXOffset?: number;
  tooltipYOffset?: number;
  toolTipItemIcon?: ComponentType<{ itemKey: string }>;
  hideAggregateContextWhenSingleSeries?: boolean;
}

const ChartTooltip = ({
  aggregateLabel,
  aggregateKey,
  colorMap,
  activeKeys = [],
  chartData,
  chartWidth = 0,
  connectNulls,
  coordinate = { x: 0, y: 0 },
  label,
  sortingFn,
  payload: payloads,
  units,
  segmentsLabel,
  tooltipWidth = 269,
  tooltipXOffset = 24,
  tooltipYOffset = 24,
  toolTipItemIcon,
  hideAggregateContextWhenSingleSeries = false,
}: ChartTooltipProps) => {
  // Use aggregateKey as fallback when no activeKeys provided
  const keysToShow = activeKeys.length > 0 ? activeKeys : aggregateKey ? [aggregateKey] : [];
  const showAggregate = aggregateKey ? keysToShow.includes(aggregateKey) : false;

  const rawPayload = payloads?.[0]?.payload;

  // When connectNulls is enabled and the hovered point has no displayable
  // values (or Recharts stripped all null-valued lines from the payload),
  // fall back to the nearest data point with real values so the tooltip
  // stays visible while hovering over interpolated line regions.
  // Recharts always passes `label` (the x-axis datetime) even when
  // payload entries are filtered out, so we use it as a position fallback.
  const currentDatetime = rawPayload?.datetime ?? (typeof label === "number" ? label : undefined);

  const hasNoDisplayableValues =
    connectNulls && currentDatetime !== undefined && !keysToShow.some((key) => isDisplayableValue(rawPayload?.[key]));

  const fallbackPayload =
    hasNoDisplayableValues && chartData
      ? findNearestDisplayablePoint(chartData, currentDatetime, keysToShow)
      : undefined;

  const payload = (fallbackPayload as typeof rawPayload) ?? rawPayload;
  const filteredEntries = payload
    ? Object.entries(payload).filter((entry): entry is [string, TooltipDisplayValue] => {
        const [key, value] = entry;
        return keysToShow.includes(key) && isDisplayableValue(value);
      })
    : [];

  // sort keys so they display in a consistent order
  const sortedEntries = sortingFn ? [...filteredEntries].sort(sortingFn) : filteredEntries;
  const aggregateValue = aggregateKey ? payload?.[aggregateKey] : undefined;
  const aggregateDisplayValue =
    aggregateValue !== null && aggregateValue !== undefined ? getDisplayValue(aggregateValue) : undefined;
  const shouldShowAggregate = Boolean(showAggregate && aggregateKey && aggregateDisplayValue !== undefined);
  const segmentEntries = sortedEntries.filter(([key]) => key !== aggregateKey);
  const hasSegmentEntries = segmentEntries.length > 0;
  const hasConfiguredSegmentKeys = keysToShow.some((key) => key !== aggregateKey);
  const showAggregateContext = !(
    hideAggregateContextWhenSingleSeries &&
    shouldShowAggregate &&
    !hasConfiguredSegmentKeys
  );

  if (payload?.datetime === undefined || (!shouldShowAggregate && !hasSegmentEntries)) {
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
              "pb-4": hasSegmentEntries,
            })}
          >
            <div>
              {showAggregateContext && (
                <div className="mb-1 text-200 text-text-primary-70">{aggregateLabel || aggregateKey}</div>
              )}
              <div className="inline-flex items-center gap-2 text-heading-100 text-text-primary">
                {showAggregateContext && (
                  <StatusCircle
                    testId={AGGREGATE_TOOLTIP_STATUS_CIRCLE_TEST_ID}
                    width="w-2"
                    status={statuses.warning}
                    variant={variants.simple}
                  />
                )}
                {aggregateDisplayValue} {units && <span>{units}</span>}
              </div>
            </div>
          </div>
        )}

        {hasSegmentEntries && (
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
