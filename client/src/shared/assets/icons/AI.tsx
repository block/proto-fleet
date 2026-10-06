import clsx from "clsx";

import { IconProps } from "./types";

type AIIconProps = IconProps & {
  innerShadow?: boolean;
};

const path = (
  <path
    d="M7.40019 2.31649L7.91063 0.785172C8.2596 -0.261722 9.7404 -0.261725 10.0894 0.785169L10.5998 2.31649C11.3999 4.71669 13.2833 6.60013 15.6835 7.40019L17.2148 7.91063C18.2617 8.2596 18.2617 9.7404 17.2148 10.0894L15.6835 10.5998C13.2833 11.3999 11.3999 13.2833 10.5998 15.6835L10.0894 17.2148C9.7404 18.2617 8.2596 18.2617 7.91063 17.2148L7.40019 15.6835C6.60013 13.2833 4.71669 11.3999 2.31649 10.5998L0.785172 10.0894C-0.261722 9.7404 -0.261725 8.2596 0.785169 7.91063L2.31649 7.40019C4.71669 6.60013 6.60013 4.71669 7.40019 2.31649Z"
    fill="currentColor"
  />
);

const AI = ({ className, innerShadow = true, width = "w-[18px]", testId }: AIIconProps) => {
  return (
    <div className={clsx(width, className)} data-testid={testId}>
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 18 18"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid meet"
      >
        {innerShadow ? (
          <>
            <g filter="url(#ai-icon-inner-shadow)">{path}</g>
            <defs>
              <filter
                id="ai-icon-inner-shadow"
                x="0"
                y="0"
                width="18"
                height="21.2283"
                filterUnits="userSpaceOnUse"
                colorInterpolationFilters="sRGB"
              >
                <feFlood floodOpacity="0" result="BackgroundImageFix" />
                <feBlend mode="normal" in="SourceGraphic" in2="BackgroundImageFix" result="shape" />
                <feColorMatrix
                  in="SourceAlpha"
                  type="matrix"
                  values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0"
                  result="hardAlpha"
                />
                <feOffset dy="3.22825" />
                <feGaussianBlur stdDeviation="1.61413" />
                <feComposite in2="hardAlpha" operator="arithmetic" k2="-1" k3="1" />
                <feColorMatrix type="matrix" values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.25 0" />
                <feBlend mode="normal" in2="shape" result="effect1_innerShadow" />
              </filter>
            </defs>
          </>
        ) : (
          path
        )}
      </svg>
    </div>
  );
};

export default AI;
