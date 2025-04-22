import { IconProps } from "./types";

const Plus = ({ className }: IconProps) => {
  return (
    <svg
      width="20"
      height="20"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={className}
    >
      <path
        className="stroke-surface-base"
        strokeOpacity=".01"
        strokeWidth=".6"
        d="M.3.3h19.4v19.4H.3z"
      />
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M10 5a1 1 0 0 1 1 1v3h3a1 1 0 1 1 0 2h-3v3a1 1 0 1 1-2 0v-3H6a1 1 0 1 1 0-2h3V6a1 1 0 0 1 1-1Z"
        fill="currentColor"
        fillOpacity=".3"
      />
    </svg>
  );
};

export default Plus;
