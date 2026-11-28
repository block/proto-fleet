import clsx from "clsx";
import { PAGE_GUTTERS, PAGE_TOP_PADDING } from "./spacing";
import type { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";

// Like the Fleet shell, own both scroll axes below the fixed ProtoOS header.
// Container units let list chrome use the scrollport width, excluding scrollbars.
const ListContentLayout = ({
  children,
  callout,
  className = "fixed inset-x-0 top-[100px] bottom-0 laptop:top-[60px] laptop:left-50",
}: ContentLayoutProps & { className?: string }) => (
  <div data-testid="list-scrollport" className={clsx("[container-type:inline-size] overflow-auto", className)}>
    <div className={`sticky left-0 w-[100cqw] ${PAGE_GUTTERS} ${PAGE_TOP_PADDING} empty:hidden`}>{callout}</div>
    {children}
  </div>
);

export default ListContentLayout;
