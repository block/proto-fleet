import { type ReactNode } from "react";
import clsx from "clsx";

interface FilterRowProps {
  children: ReactNode;
  className?: string;
  testId?: string;
}

// Sticky-left + opaque background keep the band pinned during horizontal
// scroll inside the list area beneath it.
const FilterRow = ({ children, className, testId }: FilterRowProps) => (
  <div
    className={clsx("sticky left-0 z-10 flex flex-col gap-4 bg-surface-base px-6 pt-10 laptop:px-10", className)}
    data-testid={testId}
  >
    {children}
  </div>
);

export default FilterRow;
