import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";

const DefaultContentLayout = ({ children }: ContentLayoutProps) => {
  return (
    <div className="m-14 flex justify-center phone:m-6 tablet:m-6">
      <div className="w-full max-w-[1280px]">{children}</div>
    </div>
  );
};

export default DefaultContentLayout;
