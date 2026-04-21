import clsx from "clsx";
import Stat, { type StatProps } from "@/shared/components/Stat";

type StatsPropsWithOptSize = Omit<StatProps, "size"> & Partial<Pick<StatProps, "size">>;

export type StatsProps = {
  stats: StatsPropsWithOptSize[];
  size?: StatProps["size"];
  grid?: string;
  gap?: string;
  padding?: string;
  divide?: string;
};

const Stats = ({
  stats,
  size = "medium",
  gap = "gap-x-10 gap-y-4 phone:gap-x-2",
  padding = "pt-4 pb-8 phone:pb-6",
  grid = "grid-cols-4 phone:grid-cols-2",
  divide,
}: StatsProps) => {
  return (
    <div className={clsx("grid", grid, gap, padding, divide)} data-testid="stats-container">
      {stats.map((stat) => (
        <div key={stat.label ?? stat.value} data-testid="stats-item">
          <Stat {...stat} size={size} />
        </div>
      ))}
    </div>
  );
};

export default Stats;
