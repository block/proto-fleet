import { AnimatePresence, motion } from "motion/react";
import { memo } from "react";
import clsx from "clsx";
import Stat from "@/shared/components/Stat";
import StatusCircle from "@/shared/components/StatusCircle";

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
      onClick={() => onClick && onClick(id)}
      className={clsx("relative m-0 flex-1 py-4 text-left", "phone:px-4")}
    >
      <Stat
        label={label}
        value={value}
        units={units}
        headingLevel={2}
        size="large"
      />

      <AnimatePresence>
        {isActive && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1, transition: { delay: 0.6 } }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.2 }}
            className="absolute top-4 right-0 h-[6px] w-[6px] phone:right-4"
          >
            <StatusCircle width="w-full" status="warning" variant="simple" />
          </motion.div>
        )}
      </AnimatePresence>
    </button>
  );
});

Tab.displayName = "Tab";

export default Tab;
