import { ReactNode } from "react";
import MinersStatus from "@/protoFleet/features/kpis/components/MinersStatus";
import { MinersPage } from "@/protoFleet/features/onboarding";
import { CompleteSetup } from "@/protoFleet/features/onboarding/components/CompleteSetup";
import { useOnboardingContext } from "@/protoFleet/features/onboarding/contexts/OnboardingContext";

const HomeLayout = ({ children }: { children?: ReactNode }) => {
  const { devicePaired } = useOnboardingContext();

  return (
    <div className="h-full">
      {devicePaired ? (
        <>
          <div className="flex flex-col gap-10 p-10 phone:p-6 tablet:p-6">
            <CompleteSetup />
            <div>
              <div className="flex items-center pb-6">
                <div className="grow text-heading-300">Home</div>
              </div>
              <MinersStatus
                activeMiners={10}
                offlineMiners={10}
                inactiveMiners={10}
              />
            </div>
          </div>
          {children}
        </>
      ) : (
        <MinersPage />
      )}
    </div>
  );
};

export default HomeLayout;
