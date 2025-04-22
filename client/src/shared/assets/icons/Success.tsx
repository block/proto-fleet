import { IconProps } from "./types";

const Success = ({ className }: IconProps) => {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 20 20"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={className}
    >
      <path className="fill-surface-base" fillOpacity=".01" d="M0 0h20v20H0z" />
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M10 20c5.523 0 10-4.477 10-10S15.523 0 10 0 0 4.477 0 10s4.477 10 10 10Zm4.756-12.345.655-.756-1.512-1.31-.655.756-4.548 5.249L6.65 9.84l-.76-.651-1.301 1.519.76.65 2.8 2.4a1 1 0 0 0 1.406-.104l5.2-6Z"
        fill="currentColor"
      />
    </svg>
  );
};

export default Success;
