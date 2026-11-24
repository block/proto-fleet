import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Team = ({ className, width = iconSizes.medium }: IconProps) => (
  <div className={clsx(width, className)} data-testid="team-icon">
    <svg
      width="100%"
      height="100%"
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
    >
      <circle cx="7" cy="6" r="3" />
      <path d="M1 18v-2a6 6 0 0 1 12 0v2M13 3.2a3 3 0 0 1 0 5.6M16 12a5 5 0 0 1 3 4.6V18" />
    </svg>
  </div>
);

export default Team;
