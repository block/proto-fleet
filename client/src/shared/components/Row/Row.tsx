import { ReactNode } from "react";
import clsx from "clsx";

import Divider from "@/shared/components/Divider";

interface RowProps {
  children: ReactNode;
  compact?: boolean;
  className?: string;
  divider?: boolean;
  onClick?: () => void;
  /**
   * When true, the row's interactive element renders with the native `disabled`
   * attribute (suppressing `onClick`) and an `aria-disabled` hint for assistive
   * tech. Implies a `<button>` element so screen readers still announce a
   * disabled action rather than a generic container.
   */
  disabled?: boolean;
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
  disabled,
  prefixIcon,
  suffixIcon,
  testId,
  attributes,
}: RowProps) => {
  const isInteractive = Boolean(onClick) || disabled === true;
  const Element = isInteractive ? "button" : "div";
  return (
    <div {...attributes} className={clsx("w-full")}>
      <Element
        className={clsx("peer", {
          "flex items-center gap-4": suffixIcon || prefixIcon,
          "-ml-3 w-[calc(100%+24px)] rounded-lg px-3": isInteractive,
          "hover:bg-core-primary-5": isInteractive && !disabled,
          "cursor-not-allowed opacity-50": disabled,
        })}
        onClick={disabled ? undefined : onClick}
        data-testid={testId}
        {...(Element === "button" && {
          type: "button",
          disabled: disabled === true,
          "aria-disabled": disabled === true,
        })}
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
