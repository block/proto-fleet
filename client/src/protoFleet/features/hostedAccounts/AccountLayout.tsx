import type { ReactNode } from "react";
import AccountPicker from "./components/AccountPicker";
import AppLayoutFrame from "@/protoFleet/components/AppLayout/AppLayoutFrame";
import type { NavItem } from "@/protoFleet/config/navItems";
import { Menu } from "@/shared/assets/icons";

export const AccountLayout = ({
  children,
  items,
  roleLabel,
}: {
  children: ReactNode;
  items: NavItem[];
  roleLabel: string;
}) => (
  <AppLayoutFrame
    items={items}
    roleLabel={roleLabel}
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
        <div className="ml-auto text-200 text-text-primary-50 phone:hidden">
          Account settings · across permitted sites
        </div>
      </div>
    )}
  >
    {children}
  </AppLayoutFrame>
);
