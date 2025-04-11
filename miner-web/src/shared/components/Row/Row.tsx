import { ReactNode } from "react";
import clsx from "clsx";

import Divider from "@/shared/components/Divider";

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
  attributes?: {
    [key: string]: string;
  };
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
  attributes,
}: RowProps) => {
  const Element = onClick ? "button" : "div";
  return (
    <div {...attributes} className="w-full">
      <div
        className={clsx("peer", {
          "flex items-center": suffixIcon || prefixIcon,
        })}
      >
        <div className="mr-4">{prefixIcon}</div>
        <Element
          className={clsx(
            "truncate text-left",
            { "py-2": compact },
            { "py-3": !compact },
            { "-ml-3 w-[calc(100%+24px)] rounded-lg px-3": onClick },
            { "hover:bg-core-primary-5": onClick && !isActive },
            { "w-full": !onClick },
            className,
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
            "px-4 peer-hover:invisible": onClick,
          })}
        />
      )}
    </div>
  );
};

export default Row;
