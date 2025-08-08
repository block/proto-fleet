import { ReactNode } from "react";
import useFleet from "@/protoFleet/api/useFleet";
import {
  useMinerStateCounts,
  useTotalMiners,
} from "@/protoFleet/features/fleetManagement/store/useFleetStore";
import MinersStatus from "@/protoFleet/features/kpis/components/MinersStatus";
import { MinersPage } from "@/protoFleet/features/onboarding";
import { CompleteSetup } from "@/protoFleet/features/onboarding/components/CompleteSetup";
import { useOnboardingContext } from "@/protoFleet/features/onboarding/contexts/OnboardingContext";

const HomeLayout = ({ children }: { children?: ReactNode }) => {
  const { devicePaired } = useOnboardingContext();
  useFleet(); // Ensure fleet data is loaded
  const fleetSize = useTotalMiners();
  const minerStateCounts = useMinerStateCounts();

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
                fleetSize={fleetSize ?? 1} // prevent division by zero
                activeMiners={minerStateCounts?.hashingCount ?? 0}
                offlineMiners={minerStateCounts?.offlineCount ?? 0}
                inactiveMiners={
                  (minerStateCounts?.sleepingCount ?? 0) +
                  (minerStateCounts?.brokenCount ?? 0)
                }
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
