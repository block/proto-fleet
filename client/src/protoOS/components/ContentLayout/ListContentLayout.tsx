import clsx from "clsx";
import type { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";

// Like the Fleet shell, own both scroll axes below the fixed ProtoOS header.
// Container units let list chrome use the scrollport width, excluding scrollbars.
const ListContentLayout = ({
  children,
  className = "fixed inset-x-0 top-[100px] bottom-0 laptop:top-[60px] laptop:left-50",
}: ContentLayoutProps & { className?: string }) => (
  <div data-testid="list-scrollport" className={clsx("[container-type:inline-size] overflow-auto", className)}>
    {children}
  </div>
);

export default ListContentLayout;
