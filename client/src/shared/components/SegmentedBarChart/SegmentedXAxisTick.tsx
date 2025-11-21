interface SegmentedXAxisTickProps {
  x?: number;
  y?: number;
  payload?: { value: number };
}

const SegmentedXAxisTick = ({
  x = 0,
  y = 0,
  payload,
}: SegmentedXAxisTickProps) => {
  if (!payload) return null;

  const { value } = payload;

  // Format time with am/pm
  const date = new Date(value);
  const hours = date.getHours();
  const minutes = date.getMinutes();
  const ampm = hours >= 12 ? "p" : "a";
  const displayHours = hours % 12 || 12; // Convert 0 to 12 for midnight
  const time = `${displayHours}:${minutes.toString().padStart(2, "0")}${ampm}`;

  return (
    <text
      x={x}
      y={y}
      fill="var(--color-text-primary-50)"
      fontSize={12}
      textAnchor="middle"
      dominantBaseline="hanging"
    >
      {time}
    </text>
  );
};

export default SegmentedXAxisTick;
