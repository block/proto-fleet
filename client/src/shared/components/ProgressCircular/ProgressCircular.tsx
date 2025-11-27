import clsx from "clsx";

interface ProgressCircularProps {
  className?: string;
  dataTestId?: string;
  size?: number;
  value?: number;
  indeterminate?: boolean;
}

const ProgressCircular = ({
  className,
  dataTestId,
  size = 20,
  value = 0,
  indeterminate = false,
}: ProgressCircularProps) => {
  const INDETERMINATE_VALUE = 70;

  const strokeWidth = size / 10;
  const radius = (size - strokeWidth) / 2;
  const circumference = 2 * Math.PI * radius;
  const offset = circumference - ((indeterminate ? INDETERMINATE_VALUE : value) / 100) * circumference;

  return (
    <svg
      className={clsx({ "animate-spin": indeterminate }, className)}
      xmlns="http://www.w3.org/2000/svg"
      fill="none"
      viewBox={`0 0 ${size} ${size}`}
      width={size}
      height={size}
      data-testid={dataTestId}
    >
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="currentColor"
        opacity="0.1"
        strokeWidth={strokeWidth}
      />
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="currentColor"
        strokeWidth={strokeWidth}
        strokeDasharray={circumference}
        strokeDashoffset={offset}
        strokeLinecap="round"
        transform={`rotate(-90 ${size / 2} ${size / 2})`}
      />
    </svg>
  );
};

export default ProgressCircular;
