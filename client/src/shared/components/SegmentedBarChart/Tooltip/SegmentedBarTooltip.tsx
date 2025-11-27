import type { SegmentedBarTooltipProps } from "@/shared/components/SegmentedBarChart/types";
import { getDisplayValue } from "@/shared/utils/stringUtils";

const SegmentedBarTooltip = ({
  customPayload,
  units = "",
  percentageDisplay = false,
  barPosition,
  toolTipKey,
}: SegmentedBarTooltipProps) => {
  // Don't render if no bar position or customPayload
  if (!barPosition || !customPayload) {
    return null;
  }

  let displayValue: string;

  if (toolTipKey) {
    // Find the specific segment value
    const segment = customPayload.segments?.find((seg: any) => seg.key === toolTipKey);

    if (!segment) {
      return null; // If the segment is not found, don't render the tooltip
    } else {
      const value = segment.value ?? 0;

      if (percentageDisplay) {
        // When percentageDisplay is true, the values are already percentages
        displayValue = `${Math.round(value)}%`;
      } else {
        const formatted = getDisplayValue(value);
        displayValue = formatted !== undefined ? `${formatted}${units}` : `${value}${units}`;
      }
    }
  } else {
    // Display total (default behavior)
    const total = customPayload.total || 0;
    if (percentageDisplay) {
      displayValue = "100%";
    } else {
      const formatted = getDisplayValue(total);
      displayValue = formatted !== undefined ? `${formatted}${units}` : `${total}${units}`;
    }
  }

  return (
    <div
      className="absolute rounded-md bg-white p-3 shadow-100"
      style={{
        left: `${barPosition.x}px`,
        top: `${barPosition.y - 8}px`,
        transform: "translateX(-50%) translateY(-100%)",
        zIndex: 1000,
        pointerEvents: "none",
      }}
    >
      <div className="text-heading-100 whitespace-nowrap">{displayValue}</div>
    </div>
  );
};

export default SegmentedBarTooltip;
