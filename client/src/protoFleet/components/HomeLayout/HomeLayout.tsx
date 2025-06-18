import { ReactNode } from "react";
import OfflineMiners from "@/protoFleet/features/kpis/components/OfflineMiners";

const HomeLayout = ({ children }: { children?: ReactNode }) => {
  return (
    <div>
      <div className="px-14 pt-14 phone:px-6 phone:pt-6 tablet:px-10 tablet:pt-10">
        <div className="flex items-center pb-6">
          <div className="grow text-heading-300">Home</div>
        </div>
        <OfflineMiners
          activeMiners={10}
          offlineMiners={10}
          inactiveMiners={10}
        />
      </div>
      {children}
    </div>
  );
};

export default HomeLayout;
