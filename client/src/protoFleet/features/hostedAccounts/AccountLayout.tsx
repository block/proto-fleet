import type { ReactNode } from "react";
import AccountPicker from "./components/AccountPicker";
import { useHostedAccounts } from "./HostedAccountsContext";
import AppLayoutFrame from "@/protoFleet/components/AppLayout/AppLayoutFrame";
import SecondaryNavigation from "@/protoFleet/components/SecondaryNavigation/SecondaryNavigation";
import { type NavItem, primaryNavItems, secondaryNavItems } from "@/protoFleet/config/navItems";
import { usePermissions } from "@/protoFleet/store";
import { Menu } from "@/shared/assets/icons";

export const AccountLayout = ({
  children,
  items,
  roleLabel,
}: {
  children: ReactNode;
  items: NavItem[];
  roleLabel: string;
}) => {
  const permissions = usePermissions();
  const directory = useHostedAccounts();
  const host = permissions.length > 0 || !!directory?.hostAccess;
  return (
    <AppLayoutFrame
      items={host ? primaryNavItems : items}
      roleLabel={host ? undefined : roleLabel}
      renderHeader={({ openMenu, isMenuOpen, navigationToggle }) => (
        <div className="flex h-12 items-center gap-4 px-4 laptop:h-15">
          <div className="hidden phone:block">
            <Menu
              onClick={openMenu}
              ariaLabel="Open navigation menu"
              ariaExpanded={isMenuOpen}
              testId="navigation-menu-button"
            />
          </div>
          {navigationToggle}
          <AccountPicker />
        </div>
      )}
    >
      {host ? (
        <div className="flex h-full grow flex-row">
          <SecondaryNavigation items={secondaryNavItems} parentPath="/settings" />
          <div className="min-w-0 grow">{children}</div>
        </div>
      ) : (
        children
      )}
    </AppLayoutFrame>
  );
};
