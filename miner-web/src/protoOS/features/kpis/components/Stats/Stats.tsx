import clsx from "clsx";
import Stat, { type StatProps } from "@/shared/components/Stat";

type StatsPropsWithOptSize = Omit<StatProps, "size"> &
  Partial<Pick<StatProps, "size">>;

export type StatsProps = {
  stats: StatsPropsWithOptSize[];
  size?: StatProps["size"];
  grid?: string;
  gap?: string;
  padding?: string;
};

const Stats = ({ stats, size = "medium", gap, padding, grid }: StatsProps) => {
  return (
    <div
      className={clsx(
        "grid",
        grid || "grid-cols-4 phone:grid-cols-2",
        gap || "gap-x-10 gap-y-4 phone:gap-x-2",
        padding || "pb-8 phone:pb-6",
      )}
    >
      {stats.map((stat) => (
        <div key={stat.label}>
          <Stat {...stat} size={size} />
        </div>
      ))}
    </div>
  );
};

export default Stats;
