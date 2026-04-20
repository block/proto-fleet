import clsx from "clsx";
import LocationSelector from "./LocationSelector";
import SchedulePill from "./SchedulePill";
import type { UseSchedulePillDataResult } from "./useSchedulePillData";
import { usePageBackground } from "@/protoFleet/hooks/usePageBackground";
import { Pause } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";
interface PageHeaderProps {
  isMenuOpen?: boolean;
  openMenu?: () => void;
  schedulePillData: UseSchedulePillDataResult;
}

const headerWidgetEnabled = true;

const HeaderWidgets = ({
  className,
  dismissedSetup,
  onContinueSetup,
  schedulePillData,
}: {
  className?: string;
  dismissedSetup: boolean;
  onContinueSetup: () => void;
  schedulePillData: UseSchedulePillDataResult;
}) => {
  const { pillSchedule, sections, pendingScheduleId, onToggleScheduleStatus } = schedulePillData;

  return (
    <div className={clsx("flex space-x-3", className)}>
      {pillSchedule ? (
        <SchedulePill
          pillSchedule={pillSchedule}
          sections={sections}
          pendingScheduleId={pendingScheduleId}
          onToggleScheduleStatus={onToggleScheduleStatus}
        />
      ) : null}
      {dismissedSetup ? (
        <Button variant={variants.secondary} size={sizes.compact} text="Continue setup" onClick={onContinueSetup} />
      ) : null}
    </div>
  );
};

const PageHeader = ({ isMenuOpen, openMenu, schedulePillData }: PageHeaderProps) => {
  const { isPhone, isTablet } = useWindowDimensions();
  const { bgClass } = usePageBackground();
  const [dismissedSetup, setDismissedSetup] = useReactiveLocalStorage<boolean>("completeSetupDismissed");
  const hasDismissedSetup = Boolean(dismissedSetup);

  const handleCompleteSetup = () => {
    setDismissedSetup(false);
  };

  const headerWidgetsProps = {
    dismissedSetup: hasDismissedSetup,
    onContinueSetup: handleCompleteSetup,
    schedulePillData,
  };
  const showPhoneWidgets = isPhone && (hasDismissedSetup || schedulePillData.hasVisibleSchedules);

  return (
    <>
      <div className="flex h-12 items-center laptop:h-15 desktop:h-15">
        <div className="flex grow items-center px-4">
          <div className="flex grow items-center">
            {(isPhone || isTablet) && (
              <Pause
                ariaExpanded={isMenuOpen}
                ariaLabel="Open navigation menu"
                className="mr-2 text-text-primary"
                onClick={openMenu}
                testId="navigation-menu-button"
              />
            )}
            <LocationSelector />
          </div>
          {!isPhone && headerWidgetEnabled && <HeaderWidgets {...headerWidgetsProps} />}
        </div>
      </div>
      {showPhoneWidgets && (
        <div className={clsx("flex h-[57px] items-center", bgClass)}>
          <HeaderWidgets className="ml-5" {...headerWidgetsProps} />
        </div>
      )}
    </>
  );
};

export default PageHeader;
