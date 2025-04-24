import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";

const DefaultContentLayout = ({ children }: ContentLayoutProps) => {
  return (
    <div className="m-14 flex justify-center phone:m-6 tablet:m-6">
      <div className="phone:w-[352px] tablet:w-[584px] laptop:w-[608px] desktop:w-[928px]">
        {children}
      </div>
    </div>
  );
};

export default DefaultContentLayout;
