import { ReactNode } from "react";
import clsx from "clsx";

import { type IconProps } from "./types";

type InteractiveIconProps = IconProps & {
  children: ReactNode;
};

const InteractiveIcon = ({
  ariaExpanded,
  ariaLabel,
  children,
  className,
  onClick,
  testId,
  width,
}: InteractiveIconProps) => {
  if (!onClick) {
    return (
      <div className={clsx(width, className)} data-testid={testId}>
        {children}
      </div>
    );
  }

  return (
    <button
      type="button"
      aria-expanded={ariaExpanded}
      aria-label={ariaLabel}
      className={clsx(
        "inline-flex shrink-0 items-center justify-center bg-transparent p-0 text-inherit",
        width,
        { "aspect-square": width },
        className,
        "cursor-pointer rounded-full border-0 outline-none focus-visible:ring-2 focus-visible:ring-core-primary-fill focus-visible:ring-offset-2 focus-visible:ring-offset-surface-base",
      )}
      data-testid={testId}
      onClick={onClick}
    >
      {children}
    </button>
  );
};

export default InteractiveIcon;
