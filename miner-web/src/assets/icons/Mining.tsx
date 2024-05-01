import { IconProps } from "./types";

const Mining = ({ className }: IconProps) => {
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
        d="M3.77 3.851c-.437.856-.437 1.976-.437 4.216v3.867c0 2.24 0 3.36.436 4.216a4 4 0 0 0 1.748 1.748c.856.436 1.976.436 4.216.436h.534c2.24 0 3.36 0 4.216-.436a4 4 0 0 0 1.748-1.748c.436-.856.436-1.976.436-4.216V8.067c0-2.24 0-3.36-.436-4.216a4 4 0 0 0-1.748-1.748c-.856-.436-1.976-.436-4.216-.436h-.534c-2.24 0-3.36 0-4.216.436A4 4 0 0 0 3.77 3.851Zm2.897.4a.75.75 0 1 0 0 1.5h6.666a.75.75 0 0 0 0-1.5H6.667Zm.378 4.268a.75.75 0 0 0-.756 1.296l2.961 1.727v3.042a.75.75 0 0 0 1.5 0v-3.042l2.961-1.727a.75.75 0 0 0-.755-1.296L10 10.243 7.045 8.52Z"
        fill="currentColor"
      />
    </svg>
  );
};

export default Mining;
