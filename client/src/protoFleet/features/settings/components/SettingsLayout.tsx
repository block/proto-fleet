import { ReactNode, useEffect } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import SecondaryNavigation from "@/protoFleet/components/SecondaryNavigation";
import { secondaryNavItems } from "@/protoFleet/config/navItems";
import OrgWideNotice from "@/protoFleet/features/settings/components/OrgWideNotice";
import { settingsRoutePrefetch } from "@/protoFleet/routePrefetch";
import { usePermissions } from "@/protoFleet/store";
import { prefetchRoutes } from "@/shared/utils/prefetchRoutes";

const SettingsLayout = ({ children }: { children?: ReactNode }) => {
  const { pathname } = useLocation();
  const permissions = usePermissions();
  const { activeSite } = useActiveSite({});
  // Warm sibling /settings/* tab chunks at idle.
  useEffect(() => {
    return prefetchRoutes(settingsRoutePrefetch);
  }, []);

  const currentNavItem = secondaryNavItems.find(
    (item) => pathname === item.path || pathname.startsWith(`${item.path}/`),
  );
  const requiredPermission = currentNavItem?.requiredPermission;
  if (requiredPermission && !permissions.includes(requiredPermission)) {
    return <Navigate to="/settings/general" replace />;
  }

  // Org-wide pages (everything except schedules) deliberately ignore the
  // SitePicker. Surface the notice only when it's actually informative: a
  // single site is selected (so the still-visible picker could be mistaken for
  // a filter). With "all sites" there's nothing to clarify, so we stay quiet.
  // Also require a matched settings tab — an unmatched route shouldn't claim to
  // be org-wide.
  const showOrgWideNotice = activeSite.kind === "site" && currentNavItem !== undefined && !currentNavItem.siteAware;

  return (
    <>
      <div className="flex h-full grow flex-row">
        <SecondaryNavigation items={secondaryNavItems} />
        <div className="flex min-w-0 grow flex-col p-10 phone:p-6">
          {showOrgWideNotice ? <OrgWideNotice /> : null}
          {children}
        </div>
      </div>
    </>
  );
};

export default SettingsLayout;
