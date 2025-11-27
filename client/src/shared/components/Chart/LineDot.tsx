interface LineDotProps {
  fillClassName: string;
  cx?: number;
  cy?: number;
}

const LineDot = ({ fillClassName, cx = 0, cy = 0 }: LineDotProps) => {
  return (
    <svg x={cx - 33} y={cy - 30} width="66" height="66" viewBox="0 0 66 66" fill="none">
      <g filter="url(#filter0_dddd_2194_9558)">
        <circle cx="33" cy="29" r="9" className="fill-surface-base" />
        <circle cx="33" cy="29" r="6" className={fillClassName} />
      </g>
      <defs>
        <filter
          id="filter0_dddd_2194_9558"
          x="0"
          y="0"
          width="66"
          height="66"
          filterUnits="userSpaceOnUse"
          colorInterpolationFilters="sRGB"
        >
          <feFlood floodOpacity="0" result="BackgroundImageFix" />
          <feColorMatrix
            in="SourceAlpha"
            type="matrix"
            values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0"
            result="hardAlpha"
          />
          <feOffset />
          <feGaussianBlur stdDeviation="0.5" />
          <feComposite in2="hardAlpha" operator="out" />
          <feColorMatrix type="matrix" values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.2 0" />
          <feBlend mode="normal" in2="BackgroundImageFix" result="effect1_dropShadow_2194_9558" />
          <feColorMatrix
            in="SourceAlpha"
            type="matrix"
            values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0"
            result="hardAlpha"
          />
          <feOffset dy="2" />
          <feGaussianBlur stdDeviation="2" />
          <feColorMatrix type="matrix" values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.02 0" />
          <feBlend mode="normal" in2="effect1_dropShadow_2194_9558" result="effect2_dropShadow_2194_9558" />
          <feColorMatrix
            in="SourceAlpha"
            type="matrix"
            values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0"
            result="hardAlpha"
          />
          <feOffset dy="4" />
          <feGaussianBlur stdDeviation="4" />
          <feColorMatrix type="matrix" values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.02 0" />
          <feBlend mode="normal" in2="effect2_dropShadow_2194_9558" result="effect3_dropShadow_2194_9558" />
          <feColorMatrix
            in="SourceAlpha"
            type="matrix"
            values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0"
            result="hardAlpha"
          />
          <feOffset dy="4" />
          <feGaussianBlur stdDeviation="12" />
          <feComposite in2="hardAlpha" operator="out" />
          <feColorMatrix type="matrix" values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.02 0" />
          <feBlend mode="normal" in2="effect3_dropShadow_2194_9558" result="effect4_dropShadow_2194_9558" />
          <feBlend mode="normal" in="SourceGraphic" in2="effect4_dropShadow_2194_9558" result="shape" />
        </filter>
      </defs>
    </svg>
  );
};

export default LineDot;
