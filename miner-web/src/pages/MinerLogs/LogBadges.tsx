import clsx from "clsx";

interface LogBadgesProps {
  className: string;
  count: number;
  label: string;
}

const LogBadges = ({ className, count, label }: LogBadgesProps) => {
  return (
    <div className={clsx("rounded-lg text-emphasis-300 border", className)}>
      <div className="px-2 py-[1px]">
        {count} {label}
      </div>
    </div>
  );
};

export default LogBadges;
