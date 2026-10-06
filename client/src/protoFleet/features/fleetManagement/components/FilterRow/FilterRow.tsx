import { type ReactNode } from "react";
import clsx from "clsx";

import { PAGE_SCROLL_CHROME_WIDTH } from "@/protoFleet/constants/layout";

interface FilterRowProps {
  children: ReactNode;
  className?: string;
  testId?: string;
}

// Sticky-left + opaque background keep the band pinned during horizontal page
// scroll. The explicit viewport width (PAGE_SCROLL_CHROME_WIDTH) gives it room
// to slide within the max-content page subtree instead of scrolling away.
const FilterRow = ({ children, className, testId }: FilterRowProps) => (
  <div
    className={clsx(
      "sticky left-0 z-10 flex flex-col gap-4 bg-surface-base px-6 pt-10 laptop:px-10",
      PAGE_SCROLL_CHROME_WIDTH,
      className,
    )}
    data-testid={testId}
  >
    {children}
  </div>
);

export default FilterRow;
