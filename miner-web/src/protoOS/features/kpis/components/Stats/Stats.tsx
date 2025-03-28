import clsx from "clsx";
import Stat, { type StatProps } from "@/shared/components/Stat";

type StatsPropsWithOptSize = Omit<StatProps, "size"> &
  Partial<Pick<StatProps, "size">>;

export type StatsProps = {
  stats: StatsPropsWithOptSize[];
  size?: StatProps["size"];
  gap?: string;
  wrap?: string;
  padding?: string;
  statWidth?: string;
};

const Stats = ({
  stats,
  size = "medium",
  wrap,
  statWidth,
  gap,
  padding,
}: StatsProps) => {
  return (
    <div
      className={clsx(
        "flex w-full flex-row",
        wrap || "flex-wrap",
        gap || "gap-2 gap-y-4",
        padding || "pb-8 phone:pb-6",
      )}
    >
      {stats.map((stat) => (
        <div
          key={stat.label}
          className={clsx(
            statWidth ||
              "w-[calc(25%-theme(spacing.2))] phone:min-w-[calc(50%-theme(spacing.2))]",
          )}
        >
          <Stat {...stat} size={size} />
        </div>
      ))}
    </div>
  );
};

export default Stats;
