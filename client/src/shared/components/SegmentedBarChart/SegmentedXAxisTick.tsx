interface SegmentedXAxisTickProps {
  x?: number;
  y?: number;
  payload?: { value: number };
  showDateLabel?: boolean;
  lastTickOverride?: string;
  isLastTick?: boolean;
}

const SegmentedXAxisTick = ({
  x = 0,
  y = 0,
  payload,
  showDateLabel = false,
  lastTickOverride,
  isLastTick = false,
}: SegmentedXAxisTickProps) => {
  if (!payload) return null;

  const { value } = payload;
  const date = new Date(value);

  let displayText: string;

  // If this is the last tick and we have an override, use it
  if (isLastTick && lastTickOverride) {
    displayText = lastTickOverride;
  } else if (showDateLabel) {
    // Format as date like "2/11"
    const month = date.getMonth() + 1; // Months are 0-based
    const day = date.getDate();
    displayText = `${month}/${day}`;
  } else {
    // Format time with am/pm
    const hours = date.getHours();
    const minutes = date.getMinutes();
    const ampm = hours >= 12 ? "p" : "a";
    const displayHours = hours % 12 || 12; // Convert 0 to 12 for midnight

    // If on the hour, display as "9a" instead of "9:00a"
    if (minutes === 0) {
      displayText = `${displayHours}${ampm}`;
    } else {
      displayText = `${displayHours}:${minutes.toString().padStart(2, "0")}${ampm}`;
    }
  }

  return (
    <text x={x} y={y} fill="var(--color-text-primary-50)" fontSize={12} textAnchor="middle" dominantBaseline="hanging">
      {displayText}
    </text>
  );
};

export default SegmentedXAxisTick;
