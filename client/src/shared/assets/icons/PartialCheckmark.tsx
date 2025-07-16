import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const PartialCheckmark = ({
  className,
  onClick,
  width = iconSizes.medium,
}: IconProps) => {
  return (
    <div className={clsx(width, className)} onClick={onClick}>
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 20"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
      >
        <g filter="url(#filter0_dddd_805_58063)">
          <path
            d="M13.9971 10C13.9971 10.5523 13.5494 11 12.9971 11H7C6.44772 11 6 10.5523 6 10C6 9.44772 6.44772 9 7 9H12.9971C13.5494 9 13.9971 9.44772 13.9971 10Z"
            fill="currentColor"
          />
          <path
            d="M13.9971 10C13.9971 10.5523 13.5494 11 12.9971 11H7C6.44772 11 6 10.5523 6 10C6 9.44772 6.44772 9 7 9H12.9971C13.5494 9 13.9971 9.44772 13.9971 10Z"
            fill="url(#paint0_linear_805_58063)"
            fill-opacity="0.02"
          />
        </g>
        <defs>
          <filter
            id="filter0_dddd_805_58063"
            x="-26"
            y="-11"
            width="71.9971"
            height="66"
            filterUnits="userSpaceOnUse"
            color-interpolation-filters="sRGB"
          >
            <feFlood flood-opacity="0" result="BackgroundImageFix" />
            <feColorMatrix
              in="SourceAlpha"
              type="matrix"
              values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0"
              result="hardAlpha"
            />
            <feOffset />
            <feGaussianBlur stdDeviation="0.5" />
            <feComposite in2="hardAlpha" operator="out" />
            <feColorMatrix
              type="matrix"
              values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.1 0"
            />
            <feBlend
              mode="normal"
              in2="BackgroundImageFix"
              result="effect1_dropShadow_805_58063"
            />
            <feColorMatrix
              in="SourceAlpha"
              type="matrix"
              values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0"
              result="hardAlpha"
            />
            <feOffset dy="2" />
            <feGaussianBlur stdDeviation="2" />
            <feComposite in2="hardAlpha" operator="out" />
            <feColorMatrix
              type="matrix"
              values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.04 0"
            />
            <feBlend
              mode="normal"
              in2="effect1_dropShadow_805_58063"
              result="effect2_dropShadow_805_58063"
            />
            <feColorMatrix
              in="SourceAlpha"
              type="matrix"
              values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0"
              result="hardAlpha"
            />
            <feOffset dy="8" />
            <feGaussianBlur stdDeviation="8" />
            <feComposite in2="hardAlpha" operator="out" />
            <feColorMatrix
              type="matrix"
              values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.02 0"
            />
            <feBlend
              mode="normal"
              in2="effect2_dropShadow_805_58063"
              result="effect3_dropShadow_805_58063"
            />
            <feColorMatrix
              in="SourceAlpha"
              type="matrix"
              values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0"
              result="hardAlpha"
            />
            <feOffset dy="12" />
            <feGaussianBlur stdDeviation="16" />
            <feComposite in2="hardAlpha" operator="out" />
            <feColorMatrix
              type="matrix"
              values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.04 0"
            />
            <feBlend
              mode="normal"
              in2="effect3_dropShadow_805_58063"
              result="effect4_dropShadow_805_58063"
            />
            <feBlend
              mode="normal"
              in="SourceGraphic"
              in2="effect4_dropShadow_805_58063"
              result="shape"
            />
          </filter>
          <linearGradient
            id="paint0_linear_805_58063"
            x1="9.99854"
            y1="9"
            x2="9.99854"
            y2="11"
            gradientUnits="userSpaceOnUse"
          >
            <stop stop-opacity="0" />
            <stop offset="1" />
          </linearGradient>
        </defs>
      </svg>
    </div>
  );
};

export default PartialCheckmark;
