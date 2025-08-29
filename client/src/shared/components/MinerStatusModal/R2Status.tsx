import { ReactNode } from "react";
import clsx from "clsx";

type R2StatusIconProps = {
  hasIssue: boolean;
  icon: ReactNode;
  disabled?: boolean;
};

const R2Status = ({ hasIssue, icon, disabled }: R2StatusIconProps) => {
  return (
    <div className="py-1.5">
      <div
        className={clsx("relative rounded-md p-1.5", {
          "bg-intent-critical-fill text-text-contrast": hasIssue && !disabled,
          "bg-surface-5 text-text-primary": !hasIssue,
          "opacity-30": disabled,
        })}
      >
        {icon}
      </div>
    </div>
  );
};

export default R2Status;
