import Stat, { type StatProps } from "@/shared/components/Stat";

type StatsProps = {
  stats: StatProps[];
};

const Stats = ({ stats }: StatsProps) => {
  return (
    <div className="w-full flex flex-row flex-wrap gap-2 pb-8 gap-y-4 phone:pb-6">
      {stats.map((stat) => (
        <div
          key={stat.label}
          className="w-[calc(25%-theme(spacing.2))] phone:min-w-[calc(50%-theme(spacing.2))]"
        >
          <Stat {...stat} />
        </div>
      ))}
    </div>
  );
};

export default Stats;
