import { ReactNode, useState } from "react";
import clsx from "clsx";

import NavigationMenu from "../NavigationMenu";
import { ScheduleApiProvider } from "@/protoFleet/api/ScheduleApiProvider";
import PageHeader from "@/protoFleet/components/PageHeader";
import { useSchedulePillData } from "@/protoFleet/components/PageHeader/useSchedulePillData";
import { primaryNavItems } from "@/protoFleet/config/navItems";
import { usePageBackground } from "@/protoFleet/hooks/usePageBackground";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

type Props = {
  children: ReactNode;
};

const AppLayoutContent = ({ children }: Props) => {
  const [isMenuOpen, setIsMenuOpen] = useState(false);
  const { bgClass } = usePageBackground();
  const { isPhone } = useWindowDimensions();
  const [dismissedSetup] = useReactiveLocalStorage<boolean>("completeSetupDismissed");
  const schedulePillData = useSchedulePillData();
  const hasDismissedSetup = Boolean(dismissedSetup);

  const showPhoneWidgets = isPhone && (hasDismissedSetup || schedulePillData.hasVisibleSchedules);

  return (
    <div className={clsx("absolute top-0 right-0 bottom-0 left-0", bgClass)}>
      <div className="fixed top-0 z-50 h-fit w-16 phone:w-0 tablet:w-0 desktop:w-50">
        <NavigationMenu items={primaryNavItems} isVisible={isMenuOpen} closeMenu={() => setIsMenuOpen(false)} />
      </div>

      <div
        className={`fixed top-0 right-0 bottom-[calc(100vh-theme(spacing.1)*15)] left-16 z-40 desktop:left-50 ${bgClass} phone:bottom-[calc(100vh-theme(spacing.1)*12)] phone:left-0 tablet:bottom-[calc(100vh-theme(spacing.1)*12)] tablet:left-0`}
      >
        <PageHeader isMenuOpen={isMenuOpen} openMenu={() => setIsMenuOpen(true)} schedulePillData={schedulePillData} />
      </div>

      <div
        className={clsx(
          "fixed top-[calc(theme(spacing.1)*15)] right-0 bottom-0 left-16 z-20 overflow-auto desktop:left-50",
          bgClass,
          "phone:left-0 tablet:top-[calc(theme(spacing.1)*12)] tablet:left-0",
          showPhoneWidgets ? "phone:top-[calc(theme(spacing.1)*12+57px)]" : "phone:top-[calc(theme(spacing.1)*12)]",
        )}
      >
        {children}
      </div>
    </div>
  );
};

const AppLayout = (props: Props) => (
  <ScheduleApiProvider>
    <AppLayoutContent {...props} />
  </ScheduleApiProvider>
);

export default AppLayout;
