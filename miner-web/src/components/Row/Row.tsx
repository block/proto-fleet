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
  suffixIcon?: ReactNode;
  testId?: string;
}

const Row = ({
  children,
  compact,
  className,
  divider = true,
  isActive,
  onClick,
  suffixIcon,
  testId,
}: RowProps) => {
  const Element = onClick ? "button" : "div";
  return (
    <div>
      <div className={clsx("peer", { "flex items-center": suffixIcon })}>
        <Element
          className={clsx(
            "grow overflow-scroll w-full text-left",
            { "py-2": compact },
            { "py-4": !compact },
            { "px-4 -mx-4 rounded-lg": onClick },
            { "hover:bg-surface-5": onClick && !isActive },
            className
          )}
          onClick={onClick}
          data-testid={testId}
        >
          {children}
        </Element>
        <div>
          {suffixIcon}
        </div>
      </div>
      {divider && (
        <Divider
          className={clsx("mt-[-1px]", { "peer-hover:invisible px-4 -mx-4": onClick })}
        />
      )}
    </div>
  );
};

export default Row;
