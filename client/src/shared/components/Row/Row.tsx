import { ReactNode } from "react";
import clsx from "clsx";

import Divider from "@/shared/components/Divider";

interface RowProps {
  children: ReactNode;
  compact?: boolean;
  className?: string;
  divider?: boolean;
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
  onClick,
  prefixIcon,
  suffixIcon,
  testId,
  attributes,
}: RowProps) => {
  const Element = onClick ? "button" : "div";
  return (
    <div {...attributes} className={clsx("w-full")}>
      <Element
        className={clsx("peer", {
          "flex items-center gap-4": suffixIcon || prefixIcon,
          "-ml-3 w-[calc(100%+24px)] rounded-lg px-3 hover:bg-core-primary-5": onClick,
        })}
        onClick={onClick}
        data-testid={testId}
        {...(Element === "button" && { type: "button" })}
      >
        {prefixIcon ? <div>{prefixIcon}</div> : null}
        <div
          className={clsx(
            "grow text-left",
            { "py-2": compact },
            { "py-3": !compact },
            { "w-full": !onClick },
            { "min-w-0": suffixIcon || prefixIcon },
            className,
          )}
        >
          {children}
        </div>
        {suffixIcon ? <div className="m-4">{suffixIcon}</div> : null}
      </Element>
      {divider ? (
        <Divider
          className={clsx("mt-[-1px]", {
            "px-4": onClick,
          })}
        />
      ) : null}
    </div>
  );
};

export default Row;
