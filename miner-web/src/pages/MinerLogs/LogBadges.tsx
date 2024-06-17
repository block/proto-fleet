import clsx from "clsx";

interface LogBadgesProps {
  className: string;
  count: number;
  label: string;
  wrapperClassName: string;
}

const LogBadges = ({
  className,
  count,
  label,
  wrapperClassName,
}: LogBadgesProps) => {
  return (
    <div className={clsx("flex rounded text-heading-50", wrapperClassName)}>
      <div className={clsx("px-2 py-1 rounded-s", className)}>{label}</div>
      <div className="px-2 py-1 rounded-e">{count}</div>
    </div>
  );
};

export default LogBadges;
