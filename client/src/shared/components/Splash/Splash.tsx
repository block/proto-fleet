import { ReactNode } from "react";
import { LogoAlt, Plus } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import AnimatedDotsBackground from "@/shared/components/Animation";

type SplashPanelProps = { children: ReactNode };

const SplashPanel = ({ children }: SplashPanelProps) => {
  const PlusIcon = (
    <Plus className="font-bold text-text-emphasis" width={iconSizes.small} />
  );

  return (
    <>
      <div className="flex justify-between">
        {PlusIcon}
        {PlusIcon}
      </div>
      {children}
      <div className="flex justify-between">
        {PlusIcon}
        {PlusIcon}
      </div>
    </>
  );
};

export const Splash = () => {
  return (
    <div className="flex h-screen w-full flex-col items-center justify-between bg-landing-page">
      <div className="h-1/4" />
      <div className="min-w-120 phone:min-w-80 tablet:min-w-80">
        <SplashPanel>
          <div className="mx-10 my-13 flex flex-col items-center text-display-200">
            <LogoAlt width="w-16" />
          </div>
        </SplashPanel>
      </div>
      <div className="h-1/4">
        <AnimatedDotsBackground padding={0} />
      </div>
    </div>
  );
};
