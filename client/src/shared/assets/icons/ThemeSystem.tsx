import { IconProps } from "./types";

const ThemeSystem = ({ className }: IconProps) => {
  return (
    <svg width="20" height="20" fill="none" xmlns="http://www.w3.org/2000/svg" className={className}>
      <circle
        cx="10"
        cy="10"
        r="8"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path d="M10 18a8 8 0 1 0 0-16v16Z" fill="currentColor" />
    </svg>
  );
};

export default ThemeSystem;
