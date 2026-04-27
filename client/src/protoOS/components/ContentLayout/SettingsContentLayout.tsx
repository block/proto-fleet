import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";

const SettingsContentLayout = ({ children }: ContentLayoutProps) => {
  return (
    <div className="m-6 flex justify-center laptop:m-14">
      <div className="container mx-auto h-full w-full max-w-[640px] tablet:w-[584px] laptop:w-[608px] desktop:w-full">
        {children}
      </div>
    </div>
  );
};

export default SettingsContentLayout;
