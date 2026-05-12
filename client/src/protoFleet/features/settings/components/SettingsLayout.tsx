import { ReactNode, useEffect } from "react";
import SecondaryNavigation from "@/protoFleet/components/SecondaryNavigation";
import { secondaryNavItems } from "@/protoFleet/config/navItems";
import { settingsRoutePrefetch } from "@/protoFleet/routePrefetch";
import { prefetchRoutes } from "@/shared/utils/prefetchRoutes";

const HomeLayout = ({ children }: { children?: ReactNode }) => {
  // Warm sibling /settings/* tab chunks at idle.
  useEffect(() => {
    return prefetchRoutes(settingsRoutePrefetch);
  }, []);

  return (
    <>
      <div className="flex h-full grow flex-row">
        <SecondaryNavigation items={secondaryNavItems} />
        <div className="flex min-w-0 grow flex-col p-10 phone:p-6">{children}</div>
      </div>
    </>
  );
};

export default HomeLayout;
