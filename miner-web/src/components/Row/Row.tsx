import { ReactNode } from "react";
import clsx from "clsx";
import Divider from "components/Divider";

interface RowProps {
  children: ReactNode;
  compact?: boolean;
  className?: string;
  divider?: boolean;
  isActive?: boolean;
  onClick?: () => void;
}

const Row = ({
  children,
  compact,
  className,
  divider = true,
  isActive,
  onClick,
}: RowProps) => {
  return (
    <div>
      <div
        className={clsx(
          "peer",
          { "py-2": compact },
          { "py-4": !compact },
          { "px-4 -mx-4 rounded-lg hover:cursor-pointer": onClick },
          { "hover:bg-surface-5": onClick && !isActive },
          className
        )}
        onClick={onClick}
      >
        {children}
      </div>
      {divider && <Divider className={clsx("mt-[-1px]", { "peer-hover:invisible": onClick })} />}
    </div>
  );
};

export default Row;
