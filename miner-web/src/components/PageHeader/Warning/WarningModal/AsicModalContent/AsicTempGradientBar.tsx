interface AsicTempGradientBarProps {
  maxTemp: number;
  temp: number;
}

const AsicTempGradientBar = ({ maxTemp, temp }: AsicTempGradientBarProps) => {
  const gradientWidth = 61;
  const gradientHeight = 16;
  const indicatorWidth = 6;
  const indicatorPosition = Math.max(Math.round((temp * gradientWidth) / maxTemp) - indicatorWidth, 0);

  return (
    <div className="relative">
      <svg
        width={gradientWidth}
        height={gradientHeight}
        viewBox={`0 0 ${gradientWidth} ${gradientHeight}`}
      >
        <defs>
          <linearGradient
            id="gradient"
            x1={gradientWidth}
            y1="26"
            x2="0"
            y2="26"
            gradientUnits="userSpaceOnUse"
          >
            <stop stopColor="#FA2B37" />
            <stop offset="0.25" stopColor="#FF5B00" />
            <stop offset="0.5" stopColor="#FD8A00" />
            <stop offset="0.75" stopColor="#90C300" />
            <stop offset="1" stopColor="#00A4FB" />
          </linearGradient>
        </defs>
        <rect
          fill="url(#gradient)"
          width={gradientWidth}
          height={gradientHeight}
          rx="4"
        />
      </svg>
      <div className="w-[6px] h-3 rounded shadow-100 bg-surface-base absolute top-[2px]" style={{ left: `${indicatorPosition}px` }} />
    </div>
  );
};

export default AsicTempGradientBar;
