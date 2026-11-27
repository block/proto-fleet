import { PAGE_GUTTERS, PAGE_TOP_PADDING } from "./spacing";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";

const FullScreenContentLayout = ({ children, callout }: ContentLayoutProps) => {
  return (
    <div>
      <div className={`${PAGE_GUTTERS} ${PAGE_TOP_PADDING} empty:hidden`}>{callout}</div>
      {children}
    </div>
  );
};

export default FullScreenContentLayout;
