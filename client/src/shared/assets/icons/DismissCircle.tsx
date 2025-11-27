import clsx from "clsx";
import { IconProps } from "./types";

const DismissCircle = ({ className, onClick, width = "w-[16px]" }: IconProps) => {
  return (
    <div className={clsx(width, className)} onClick={onClick}>
      <svg width="100%" height="100%" fill="none" viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg">
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M8 16A8 8 0 1 0 8 0a8 8 0 0 0 0 16ZM5.707 4.293a1 1 0 0 0-1.414 1.414L6.586 8l-2.293 2.293a1 1 0 1 0 1.414 1.414L8 9.414l2.293 2.293a1 1 0 0 0 1.414-1.414L9.414 8l2.293-2.293a1 1 0 0 0-1.414-1.414L8 6.586 5.707 4.293Z"
          fill="currentColor"
          fillOpacity=".3"
        />
      </svg>
    </div>
  );
};

export default DismissCircle;
