import { ReactNode, useState } from "react";
import { useLocation } from "react-router-dom";
import clsx from "clsx";

import NavigationMenu from "../NavigationMenu";
import PageHeader from "@/protoFleet/components/PageHeader";
import { primaryNavItems } from "@/protoFleet/config/navItems";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

type Props = {
  children: ReactNode;
};

const AppLayout = ({ children }: Props) => {
  const [isMenuOpen, setIsMenuOpen] = useState(false);
  const location = useLocation();
  const isDashboard = location.pathname === "/";
  const { isPhone } = useWindowDimensions();
  const [dismissedSetup] = useReactiveLocalStorage<boolean>(
    "completeSetupDismissed",
  );

  const showPhoneWidgets = isPhone && dismissedSetup;

  return (
    <div className="absolute top-0 right-0 bottom-0 left-0 bg-surface-base">
      <div className="fixed top-0 z-50 h-fit w-16 phone:w-0 tablet:w-0">
        <NavigationMenu
          items={primaryNavItems}
          isVisible={isMenuOpen}
          closeMenu={() => setIsMenuOpen(false)}
        />
      </div>

      <div
        className={`fixed top-0 right-0 bottom-[calc(100vh-theme(spacing.1)*15)] left-16 z-40 ${isDashboard ? "bg-surface-5" : "bg-surface-base"} phone:bottom-[calc(100vh-theme(spacing.1)*12)] phone:left-0 tablet:bottom-[calc(100vh-theme(spacing.1)*12)] tablet:left-0`}
      >
        <PageHeader openMenu={() => setIsMenuOpen(true)} />
      </div>

      <div
        className={clsx(
          "fixed top-[calc(theme(spacing.1)*15)] right-0 bottom-0 left-16 z-20 overflow-auto",
          isDashboard ? "bg-surface-5" : "bg-surface-base",
          "phone:left-0 tablet:top-[calc(theme(spacing.1)*12)] tablet:left-0",
          showPhoneWidgets
            ? "phone:top-[calc(theme(spacing.1)*12+57px)]"
            : "phone:top-[calc(theme(spacing.1)*12)]",
        )}
      >
        {children}
      </div>
    </div>
  );
};

export default AppLayout;
