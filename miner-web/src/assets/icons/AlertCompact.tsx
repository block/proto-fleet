import { IconProps } from "./types";

const AlertCompact = ({ className }: IconProps) => {
  return (
    <svg
      width="8"
      height="8"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={className}
    >
      <path
        d="M1.329 3.073C2.294 1.365 2.777.511 3.439.291a1.778 1.778 0 0 1 1.122 0c.662.22 1.145 1.074 2.11 2.782.966 1.709 1.449 2.563 1.304 3.259-.08.383-.276.73-.561.992C6.896 7.8 5.931 7.8 4 7.8c-1.93 0-2.896 0-3.414-.476a1.863 1.863 0 0 1-.56-.992c-.146-.696.337-1.55 1.303-3.259Z"
        fill="currentColor"
      />
    </svg>
  );
};

export default AlertCompact;
