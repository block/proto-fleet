import { type ReactNode } from "react";
import clsx from "clsx";

interface FilterRowProps {
  children: ReactNode;
  // Optional pass-through so callers can add testIds, extra spacing, etc.
  className?: string;
  testId?: string;
}

// Standard filter / action band that sits between the Fleet tab strip and a
// list. Owns the vertical spacing between the tab nav (which renders its own
// pt-6) and the band content via `pt-10`; the list below provides its own
// `pt-6` (typically via `FleetListWrapper`) so each element controls its own
// top padding. Horizontal padding mirrors `MinerList` / `RacksPage` so list
// rows align to the same left edge as the filter content.
const FilterRow = ({ children, className, testId }: FilterRowProps) => (
  <div className={clsx("flex flex-col gap-4 px-6 pt-10 laptop:px-10", className)} data-testid={testId}>
    {children}
  </div>
);

export default FilterRow;
