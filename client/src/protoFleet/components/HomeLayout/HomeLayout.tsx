import { ReactNode } from "react";
import useFleet from "@/protoFleet/api/useFleet";
import MinersStatus from "@/protoFleet/features/kpis/components/MinersStatus";
import { MinersPage } from "@/protoFleet/features/onboarding";
import { CompleteSetup } from "@/protoFleet/features/onboarding/components/CompleteSetup";
import { useDeviceStatusCounts, useTotalMiners } from "@/protoFleet/store";
import { useDevicePaired } from "@/protoFleet/store";

const HomeLayout = ({ children }: { children?: ReactNode }) => {
  const devicePaired = useDevicePaired();
  useFleet({ scope: "global", mode: "metadata" }); // Ensure fleet data is loaded
  const fleetSize = useTotalMiners();
  const deviceStatusCounts = useDeviceStatusCounts();

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
                activeMiners={deviceStatusCounts?.hashingCount ?? 0}
                offlineMiners={deviceStatusCounts?.offlineCount ?? 0}
                inactiveMiners={
                  (deviceStatusCounts?.sleepingCount ?? 0) +
                  (deviceStatusCounts?.brokenCount ?? 0)
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
