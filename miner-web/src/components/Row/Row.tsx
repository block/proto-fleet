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
  prefixIcon?: ReactNode;
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
  prefixIcon,
  suffixIcon,
  testId,
}: RowProps) => {
  const Element = onClick ? "button" : "div";
  return (
    <div>
      <div className={clsx("peer", { "flex items-center": suffixIcon || prefixIcon })}>
        <div className="mr-4">{prefixIcon}</div>
        <Element
          className={clsx(
            "text-left truncate",
            { "py-2": compact },
            { "py-3": !compact },
            { "px-3 -ml-3 rounded-lg w-[calc(100%+24px)]": onClick },
            { "hover:bg-core-primary-5": onClick && !isActive },
            { "w-full": !onClick },
            className
          )}
          onClick={onClick}
          data-testid={testId}
        >
          {children}
        </Element>
        <div>{suffixIcon}</div>
      </div>
      {divider && (
        <Divider
          className={clsx("mt-[-1px]", {
            "peer-hover:invisible px-4": onClick,
          })}
        />
      )}
    </div>
  );
};

export default Row;
