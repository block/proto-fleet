import { PAGE_PADDING } from "./spacing";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";

const DefaultContentLayout = ({ children, callout }: ContentLayoutProps) => (
  <div className={PAGE_PADDING}>
    <div className="mb-6 empty:hidden">{callout}</div>
    {children}
  </div>
);

export default DefaultContentLayout;
