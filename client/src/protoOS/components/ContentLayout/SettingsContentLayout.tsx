import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";

const SettingsContentLayout = ({ children }: ContentLayoutProps) => {
  return (
    <div className="m-14 flex justify-center phone:m-6 tablet:m-6">
      <div className="container mx-auto h-full max-w-[640px] phone:w-full tablet:w-[584px] laptop:w-[608px]">
        {children}
      </div>
    </div>
  );
};

export default SettingsContentLayout;
