import type { ReactNode } from "react";
import { useLocation } from "react-router-dom";
import AccountPicker from "./components/AccountPicker";
import { useHostedAccounts } from "./HostedAccountsContext";
import AppLayoutFrame from "@/protoFleet/components/AppLayout/AppLayoutFrame";
import SecondaryNavigation from "@/protoFleet/components/SecondaryNavigation/SecondaryNavigation";
import { type NavItem, primaryNavItems, secondaryNavItems } from "@/protoFleet/config/navItems";
import { usePermissions } from "@/protoFleet/store";
import { Dismiss, Menu } from "@/shared/assets/icons";
import Header from "@/shared/components/Header";
import Modal from "@/shared/components/Modal";

export const AccountLayout = ({
  children,
  items,
  roleLabel,
  workspace = false,
  previewName,
  onPreviewClose,
}: {
  children: ReactNode;
  items: NavItem[];
  roleLabel: string;
  workspace?: boolean;
  previewName?: string;
  onPreviewClose?: () => void;
}) => {
  const permissions = usePermissions();
  const directory = useHostedAccounts();
  const host = !workspace && (permissions.length > 0 || !!directory?.hostAccess);
  const { pathname } = useLocation();
  const settings = items.find((item) => item.children?.length);
  const accountSettings = settings?.children ?? [];
  const onAccountSettings = accountSettings.some(
    (item) => pathname === item.path || pathname.startsWith(`${item.path}/`),
  );
  const layout = (
    <AppLayoutFrame
      contained={!!previewName}
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
          {previewName ? null : <AccountPicker />}
        </div>
      )}
    >
      {host || onAccountSettings ? (
        <div className="flex h-full grow flex-row">
          <SecondaryNavigation
            items={host ? secondaryNavItems : accountSettings}
            parentPath={host ? "/settings" : settings?.path}
          />
          <div className="min-w-0 grow">{children}</div>
        </div>
      ) : (
        children
      )}
    </AppLayoutFrame>
  );
  return previewName ? (
    <Modal
      open
      size="fullscreen"
      showHeader={false}
      dialogLabel={`Viewing as ${previewName}`}
      manageFocus
      onDismiss={onPreviewClose}
      className="!p-0"
      bodyClassName="flex h-full min-h-0 flex-col bg-surface-base"
    >
      <div className="shrink-0 border-b border-border-5 px-6 py-4">
        <Header
          inline
          icon={<Dismiss />}
          iconAriaLabel="Close client preview"
          iconOnClick={() => onPreviewClose?.()}
          title={
            <span className="flex min-w-0 flex-wrap items-center gap-3">
              <span className="truncate text-heading-100 text-text-primary">Viewing as {previewName}</span>
              <span className="rounded-full bg-core-primary-5 px-3 py-1 text-200 font-normal text-text-primary-70">
                Read-only
              </span>
            </span>
          }
        />
      </div>
      <div className="relative min-h-0 flex-1 [transform:translateZ(0)]">{layout}</div>
    </Modal>
  ) : (
    layout
  );
};
