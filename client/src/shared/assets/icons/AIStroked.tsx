import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const AIStroked = ({ className, width = iconSizes.medium, testId }: IconProps) => {
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
        <path
          d="M7.57795 3.0591L8.03167 1.69793C8.34186 0.767358 9.65813 0.767356 9.96832 1.69793L10.422 3.0591C11.1332 5.19261 12.8074 6.86678 14.9409 7.57795L16.3021 8.03167C17.2326 8.34186 17.2326 9.65813 16.3021 9.96832L14.9409 10.422C12.8074 11.1332 11.1332 12.8074 10.422 14.9409L9.96833 16.3021C9.65813 17.2326 8.34187 17.2326 8.03167 16.3021L7.57795 14.9409C6.86678 12.8074 5.19261 11.1332 3.0591 10.422L1.69793 9.96833C0.767358 9.65813 0.767356 8.34187 1.69793 8.03167L3.0591 7.57795C5.19261 6.86678 6.86678 5.19261 7.57795 3.0591Z"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinejoin="round"
        />
      </svg>
    </div>
  );
};

export default AIStroked;
