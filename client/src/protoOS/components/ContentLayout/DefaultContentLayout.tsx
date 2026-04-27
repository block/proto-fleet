import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";

const DefaultContentLayout = ({ children }: ContentLayoutProps) => {
  return (
    <div className="m-6 flex justify-center laptop:m-14">
      <div className="w-full max-w-[1280px]">{children}</div>
    </div>
  );
};

export default DefaultContentLayout;
