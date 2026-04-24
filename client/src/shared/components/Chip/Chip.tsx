import { ReactNode } from "react";
import clsx from "clsx";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface ChipProps {
  loading?: boolean;
  prefixIcon?: ReactNode;
  children?: ReactNode;
  onClick?: () => void;
}

const Chip = ({ loading, prefixIcon, children, onClick }: ChipProps) => {
  const prefix = loading ? <ProgressCircular indeterminate size={16} /> : prefixIcon;

  return (
    <div
      className={clsx("flex w-fit items-center rounded border border-border-5 px-2 py-1", {
        "cursor-pointer": onClick,
      })}
      onClick={() => onClick && onClick()}
    >
      {prefix}
      {children && prefix ? <span className="w-1" /> : null}
      <span className="text-200">{children}</span>
    </div>
  );
};

export default Chip;
