import { iconSizes } from "./constants";
import InteractiveIcon from "./InteractiveIcon";
import { IconProps } from "./types";

const Checkmark = ({ width = iconSizes.medium, ...props }: IconProps) => {
  return (
    <InteractiveIcon {...props} testId="checkmark-icon" width={width}>
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 20"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid meet"
      >
        <path fill="currentColor" fillOpacity=".01" d="M4 4h12v12H4z" />
        <g filter="url(#a)" fillRule="evenodd" clipRule="evenodd">
          <path
            d="m15.413 6.939-.676.737-5.5 6a1 1 0 0 1-1.444.031l-2.5-2.5-.707-.707L6 9.086l.707.707 1.761 1.761 4.795-5.23.675-.737 1.475 1.352Z"
            fill="currentColor"
          />
          <path
            d="m15.413 6.939-.676.737-5.5 6a1 1 0 0 1-1.444.031l-2.5-2.5-.707-.707L6 9.086l.707.707 1.761 1.761 4.795-5.23.675-.737 1.475 1.352Z"
            fill="url(#b)"
            fillOpacity=".02"
          />
        </g>
        <defs>
          <linearGradient id="b" x1="9.999" y1="5.587" x2="9.999" y2="14" gradientUnits="userSpaceOnUse">
            <stop stopOpacity="0" />
            <stop offset="1" />
          </linearGradient>
          <filter
            id="a"
            x="-27.414"
            y="-14.413"
            width="74.827"
            height="72.413"
            filterUnits="userSpaceOnUse"
            colorInterpolationFilters="sRGB"
          >
            <feFlood floodOpacity="0" result="BackgroundImageFix" />
            <feColorMatrix in="SourceAlpha" values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0" result="hardAlpha" />
            <feOffset />
            <feGaussianBlur stdDeviation=".5" />
            <feComposite in2="hardAlpha" operator="out" />
            <feColorMatrix values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.1 0" />
            <feBlend in2="BackgroundImageFix" result="effect1_dropShadow_531_606" />
            <feColorMatrix in="SourceAlpha" values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0" result="hardAlpha" />
            <feOffset dy="2" />
            <feGaussianBlur stdDeviation="2" />
            <feComposite in2="hardAlpha" operator="out" />
            <feColorMatrix values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.04 0" />
            <feBlend in2="effect1_dropShadow_531_606" result="effect2_dropShadow_531_606" />
            <feColorMatrix in="SourceAlpha" values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0" result="hardAlpha" />
            <feOffset dy="8" />
            <feGaussianBlur stdDeviation="8" />
            <feComposite in2="hardAlpha" operator="out" />
            <feColorMatrix values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.02 0" />
            <feBlend in2="effect2_dropShadow_531_606" result="effect3_dropShadow_531_606" />
            <feColorMatrix in="SourceAlpha" values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0" result="hardAlpha" />
            <feOffset dy="12" />
            <feGaussianBlur stdDeviation="16" />
            <feComposite in2="hardAlpha" operator="out" />
            <feColorMatrix values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.04 0" />
            <feBlend in2="effect3_dropShadow_531_606" result="effect4_dropShadow_531_606" />
            <feBlend in="SourceGraphic" in2="effect4_dropShadow_531_606" result="shape" />
          </filter>
        </defs>
      </svg>
    </InteractiveIcon>
  );
};

export default Checkmark;
