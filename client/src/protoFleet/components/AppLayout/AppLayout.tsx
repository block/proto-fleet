import { type ReactElement, type ReactNode, useState } from "react";
import clsx from "clsx";

import NavigationMenu from "../NavigationMenu";
import { ScheduleApiProvider } from "@/protoFleet/api/ScheduleApiProvider";
import PageHeader from "@/protoFleet/components/PageHeader";
import { hasVisibleHeaderWidgets } from "@/protoFleet/components/PageHeader/headerWidgetVisibility";
import { useCurtailmentPillData } from "@/protoFleet/components/PageHeader/useCurtailmentPillData";
import { useSchedulePillData } from "@/protoFleet/components/PageHeader/useSchedulePillData";
import { primaryNavItems } from "@/protoFleet/config/navItems";
import { usePageBackground } from "@/protoFleet/hooks/usePageBackground";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

type Props = {
  children: ReactNode;
};

function AppLayoutContent({ children }: Props): ReactElement {
  const [isMenuOpen, setIsMenuOpen] = useState(false);
  const { bgClass } = usePageBackground();
  const { isPhone } = useWindowDimensions();
  const [dismissedSetup] = useReactiveLocalStorage<boolean>("completeSetupDismissed");
  const curtailmentPillData = useCurtailmentPillData();
  const schedulePillData = useSchedulePillData();
  const hasDismissedSetup = Boolean(dismissedSetup);
  const hasHeaderWidgets = hasVisibleHeaderWidgets({
    hasDismissedSetup,
    hasActiveCurtailment: curtailmentPillData.activeEvent !== null,
    hasVisibleSchedules: schedulePillData.hasVisibleSchedules,
  });

  const showPhoneWidgets = isPhone && hasHeaderWidgets;

  return (
    <div className={clsx("absolute top-0 right-0 bottom-0 left-0", bgClass)}>
      <div className="fixed top-0 z-50 h-fit w-0 laptop:w-16 desktop:w-50">
        <NavigationMenu items={primaryNavItems} isVisible={isMenuOpen} closeMenu={() => setIsMenuOpen(false)} />
      </div>

      <div
        className={`fixed top-0 right-0 bottom-[calc(100vh-theme(spacing.1)*12)] left-0 z-40 laptop:bottom-[calc(100vh-theme(spacing.1)*15)] laptop:left-16 desktop:left-50 ${bgClass}`}
      >
        <PageHeader
          isMenuOpen={isMenuOpen}
          openMenu={() => setIsMenuOpen(true)}
          curtailmentPillData={curtailmentPillData}
          schedulePillData={schedulePillData}
        />
      </div>

      <div
        className={clsx(
          "fixed top-[calc(theme(spacing.1)*12)] right-0 bottom-0 left-0 z-20 overflow-auto laptop:top-[calc(theme(spacing.1)*15)] laptop:left-16 desktop:left-50",
          bgClass,
          showPhoneWidgets ? "phone:top-[calc(theme(spacing.1)*12+57px)]" : "phone:top-[calc(theme(spacing.1)*12)]",
        )}
      >
        {children}
      </div>
    </div>
  );
}

function AppLayout(props: Props): ReactElement {
  return (
    <ScheduleApiProvider>
      <AppLayoutContent {...props} />
    </ScheduleApiProvider>
  );
}

export default AppLayout;
