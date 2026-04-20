import { type IconProps } from "./types";

const Triangle = ({ className }: IconProps) => (
  <svg width="14" height="12" viewBox="0 0 14 12" fill="none" xmlns="http://www.w3.org/2000/svg" className={className}>
    <path d="M7 0L14 12H0L7 0Z" fill="currentColor" />
  </svg>
);

export default Triangle;
