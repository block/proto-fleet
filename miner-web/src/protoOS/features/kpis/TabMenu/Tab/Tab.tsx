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
        "w-[calc(25%-theme(spacing.2))] relative m-0 p-4 flex-1 text-left rounded-2xl phone:min-w-[calc(50%-theme(spacing.2))]",
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
        <div className="absolute right-4 top-4 w-[6px] h-[6px]">
          <StatusCircle width="w-full" status="warning" variant="simple" />
        </div>
      )}
    </button>
  );
};

export default Tab;
