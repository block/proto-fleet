import { ReactNode } from "react";

import GlobalActionsWidget from "./GlobalActions";
import MinerStatus from "./MinerStatus";
import PoolStatus from "./PoolStatus";
import PowerWidget from "./Power";
import PowerTarget from "./PowerTarget";
import FirmwareUpdateStatus from "@/protoOS/features/firmwareUpdate/components/FirmwareUpdateStatus";
import { Pause } from "@/shared/assets/icons";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface PageHeaderProps {
  customButtons?: ReactNode;
  openMenu?: () => void;
  title: string;
}

const MobileHeader = ({ openMenu, title, customButtons }: PageHeaderProps) => {
  return (
    <div className="w-full">
      {/* Top bar */}
      <div className="flex h-12 w-full items-center justify-between gap-2 self-start px-4 py-2">
        <div className="inline-flex items-center gap-2">
          <Pause
            ariaLabel="Open navigation menu"
            className="text-text-primary"
            onClick={openMenu}
            testId="navigation-menu-button"
          />
          <div className="text-300 text-text-primary-70">{title}</div>
        </div>
        <div className="inline-flex items-center gap-2">
          {customButtons ?? (
            <>
              <MinerStatus />
              <PowerWidget />
              <GlobalActionsWidget />
            </>
          )}
        </div>
      </div>
      {/* Bottom bar */
      /* If custom buttons are passed (Onboarding flow), dont render bottom bar*/}
      {!customButtons && (
        <div className="scrollbar-hide flex w-full gap-2 overflow-auto px-4 pb-6">
          <FirmwareUpdateStatus />
          <PowerTarget />
          <PoolStatus />
        </div>
      )}
    </div>
  );
};

const DesktopHeader = ({ customButtons }: { customButtons: PageHeaderProps["customButtons"] }) => {
  return (
    <div className="flex w-full items-center justify-end gap-4 pl-60">
      <div className="flex grow justify-between space-x-3 self-center px-4 [scrollbar-width:none]">
        <div className="flex space-x-3 phone:flex-shrink-0">
          {!customButtons && (
            <>
              <MinerStatus />
              <FirmwareUpdateStatus />
            </>
          )}
        </div>

        <div className="flex space-x-3 phone:flex-shrink-0">
          {customButtons ?? (
            <>
              <PowerTarget />
              <PoolStatus />
              <PowerWidget />
              <GlobalActionsWidget />
            </>
          )}
        </div>
      </div>
    </div>
  );
};

const PageHeader = ({ customButtons, openMenu, title }: PageHeaderProps) => {
  const { isPhone, isTablet } = useWindowDimensions();

  return (
    <div
      className="fixed top-0 right-0 left-0 z-20 flex h-[60px] bg-surface-base phone:h-fit tablet:h-fit"
      data-testid="page-header"
    >
      {isPhone || isTablet ? (
        <MobileHeader openMenu={openMenu} title={title} customButtons={customButtons} />
      ) : (
        <DesktopHeader customButtons={customButtons} />
      )}
    </div>
  );
};

export default PageHeader;
