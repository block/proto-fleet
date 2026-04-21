import { IconProps } from "./types";

const Minus = ({ className }: IconProps) => {
  return (
    <svg width="20" height="20" fill="none" xmlns="http://www.w3.org/2000/svg" className={className}>
      <path className="stroke-surface-base" strokeOpacity=".01" strokeWidth=".6" d="M.3.3h19.4v19.4H.3z" />
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M5 10a1 1 0 0 1 1-1h8a1 1 0 1 1 0 2H6a1 1 0 0 1-1-1Z"
        fill="currentColor"
        fillOpacity=".3"
      />
    </svg>
  );
};

export default Minus;
