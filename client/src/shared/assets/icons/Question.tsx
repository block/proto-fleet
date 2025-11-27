import { IconProps } from "./types";

const Question = ({ className }: IconProps) => {
  return (
    <svg width="16" height="16" fill="none" xmlns="http://www.w3.org/2000/svg" className={className}>
      <path className="fill-surface-base" fillOpacity=".01" d="M0 0h16v16H0z" />
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M8 16A8 8 0 1 0 8 0a8 8 0 0 0 0 16ZM7.304 5.422A1.248 1.248 0 0 1 9.184 6.5v.002c0 .218-.177.52-.678.854a4.284 4.284 0 0 1-.88.443l-.009.003h.002a1 1 0 0 0 .634 1.897l-.317-.949.317.948h.002l.004-.001.01-.004.028-.01a6.123 6.123 0 0 0 .397-.16 6.28 6.28 0 0 0 .921-.503c.623-.415 1.57-1.238 1.57-2.517l-1-.001h1A3.248 3.248 0 0 0 4.872 5.42a1 1 0 1 0 1.887.664c.098-.278.29-.513.545-.662Zm.661 5.328a1 1 0 1 0 0 2h.01a1 1 0 1 0 0-2h-.01Z"
        fill="currentColor"
        fillOpacity=".3"
      />
    </svg>
  );
};

export default Question;
