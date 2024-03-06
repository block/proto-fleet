import { IconProps } from "./types";

const Info = ({ className }: IconProps) => {
  return (
    <svg width="20" height="20" fill="none" xmlns="http://www.w3.org/2000/svg" className={className}>
      <path
        fill="#fff"
        fillOpacity=".01"
        d="M0 0h20v20H0z"
      />
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M19 10a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-8-4.25a1 1 0 1 1-2 0 1 1 0 0 1 2 0Zm0 8.5v-6H9v6h2Z"
        fill="currentColor"
      />
    </svg>
  );
};

export default Info;
