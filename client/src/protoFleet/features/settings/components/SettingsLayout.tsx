import { ReactNode, useEffect } from "react";
import { Navigate, useLocation } from "react-router-dom";
import SecondaryNavigation from "@/protoFleet/components/SecondaryNavigation";
import { secondaryNavItems } from "@/protoFleet/config/navItems";
import { settingsRoutePrefetch } from "@/protoFleet/routePrefetch";
import { useOrgPermissions } from "@/protoFleet/store";
import { prefetchRoutes } from "@/shared/utils/prefetchRoutes";

const SettingsLayout = ({ children }: { children?: ReactNode }) => {
  const { pathname } = useLocation();
  const orgPermissions = useOrgPermissions();
  // Warm sibling /settings/* tab chunks at idle.
  useEffect(() => {
    return prefetchRoutes(settingsRoutePrefetch);
  }, []);

  const currentNavItem = secondaryNavItems.find(
    (item) => pathname === item.path || pathname.startsWith(`${item.path}/`),
  );
  const requiredPermission = currentNavItem?.requiredPermission;
  if (requiredPermission && !orgPermissions.includes(requiredPermission)) {
    return <Navigate to="/settings/general" replace />;
  }

  return (
    <>
      <div className="flex h-full grow flex-row">
        <SecondaryNavigation items={secondaryNavItems} />
        <div className="flex min-w-0 grow flex-col p-10 phone:p-6">{children}</div>
      </div>
    </>
  );
};

export default SettingsLayout;
