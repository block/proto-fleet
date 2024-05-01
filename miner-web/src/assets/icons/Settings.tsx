import { IconProps } from "./types";

const Settings = ({ className }: IconProps) => {
  return (
    <svg
      width="20"
      height="20"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={className}
    >
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M15.598 3.304A3 3 0 0 0 13 1.804H7a3 3 0 0 0-2.598 1.5l-3 5.196a3 3 0 0 0 0 3l3 5.196A3 3 0 0 0 7 18.196h6a3 3 0 0 0 2.598-1.5l3-5.196a3 3 0 0 0 0-3l-3-5.196ZM10 13a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z"
        fill="currentColor"
      />
    </svg>
  );
};

export default Settings;
