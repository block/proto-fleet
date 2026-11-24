import type { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";

// Like the Fleet shell, own both scroll axes below the fixed ProtoOS header.
// Container units let list chrome use the scrollport width, excluding scrollbars.
const ListContentLayout = ({ children }: ContentLayoutProps) => (
  <div className="[container-type:inline-size] fixed inset-x-0 top-[100px] bottom-0 overflow-auto laptop:top-[60px] laptop:left-50">
    {children}
  </div>
);

export default ListContentLayout;
