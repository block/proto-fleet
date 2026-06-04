import { type ReactNode } from "react";
import clsx from "clsx";

interface FilterRowProps {
  children: ReactNode;
  className?: string;
  testId?: string;
}

// Standard filter / action band that sits between the Fleet tab strip and a
// list. Horizontal padding mirrors`MinerList` / `RacksPage` so list rows align
// to the same left edge as thefilter content. `sticky left-0` + opaque background
// keep the band pinned during any residual horizontal scroll inside the page.
const FilterRow = ({ children, className, testId }: FilterRowProps) => (
  <div
    className={clsx("sticky left-0 z-10 flex flex-col gap-4 bg-surface-base px-6 pt-10 laptop:px-10", className)}
    data-testid={testId}
  >
    {children}
  </div>
);

export default FilterRow;
