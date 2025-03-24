import clsx from "clsx";
import Stat from "@/shared/components/Stat";
import StatusCircle from "@/shared/components/StatusCircle";

type TabProps = {
  id: string;
  label: string;
  value?: number;
  units: string;
  path: string;
  isActive?: boolean;
  onClick?: (id: string) => void;
};

const Tab = ({ id, label, value, units, isActive, onClick }: TabProps) => {
  return (
    <button
      onClick={() => onClick && onClick(id)}
      className={clsx(
        "relative m-0 w-[calc(25%-theme(spacing.2))] flex-1 rounded-2xl p-4 text-left phone:min-w-[calc(50%-theme(spacing.2))]",
        isActive && "bg-surface-base shadow-100",
      )}
    >
      <Stat
        label={label}
        value={value}
        units={units}
        headingLevel={2}
        size="large"
      />

      {isActive && (
        <div className="absolute top-4 right-4 h-[6px] w-[6px]">
          <StatusCircle width="w-full" status="warning" variant="simple" />
        </div>
      )}
    </button>
  );
};

export default Tab;
