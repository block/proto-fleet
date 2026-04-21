import { memo } from "react";
import clsx from "clsx";
import Stat from "@/shared/components/Stat";

type TabProps = {
  id: string;
  label: string;
  value?: number | string;
  units?: string;
  path: string;
  isActive?: boolean;
  onClick?: (id: string) => void;
};

// Use memo to prevent re-rendering when parent components change but this component's props don't
const Tab = memo(({ id, label, value, units, isActive, onClick }: TabProps) => {
  return (
    <button
      role="tab"
      aria-selected={isActive}
      onClick={() => onClick && onClick(id)}
      className={clsx(
        "relative m-0 flex-1 p-6 text-left phone:p-4",
        "rounded-2xl",
        isActive ? "bg-core-primary-fill **:text-text-contrast **:opacity-100" : "bg-core-primary-5",
      )}
      data-testid={`tab-${id}`}
    >
      <Stat label={label} value={value} units={units} headingLevel={2} size="large" />
    </button>
  );
});

Tab.displayName = "Tab";

export default Tab;
