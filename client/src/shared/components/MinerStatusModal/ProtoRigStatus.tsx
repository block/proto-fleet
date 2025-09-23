import { ReactNode } from "react";
import clsx from "clsx";

type ProtoRigStatusIconProps = {
  hasIssue: boolean;
  icon: ReactNode;
  disabled?: boolean;
};

const ProtoRigStatus = ({
  hasIssue,
  icon,
  disabled,
}: ProtoRigStatusIconProps) => {
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

export default ProtoRigStatus;
