import { useLocation } from "react-router-dom";
import clsx from "clsx";
import AlertStatus from "./AlertStatus";
import LocationSelector from "./LocationSelector";
import { Pause } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";
interface PageHeaderProps {
  openMenu?: () => void;
}

const headerWidgetEnabled = true;

const HeaderWidgets = ({ className }: { className?: string }) => {
  const [dismissedSetup, setDismissedSetup] = useReactiveLocalStorage<boolean>(
    "completeSetupDismissed",
  );
  const handleCompleteSetup = () => {
    setDismissedSetup(false);
  };

  return (
    <div className={clsx("flex space-x-3", className)}>
      <AlertStatus />
      {dismissedSetup && (
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text="Continue setup"
          onClick={handleCompleteSetup}
        />
      )}
    </div>
  );
};

const PageHeader = ({ openMenu }: PageHeaderProps) => {
  const { isPhone, isTablet } = useWindowDimensions();
  const location = useLocation();
  const [dismissedSetup] = useReactiveLocalStorage<boolean>(
    "completeSetupDismissed",
  );

  const showPhoneWidgets = isPhone && dismissedSetup;
  const isDashboard = location.pathname === "/";

  return (
    <>
      <div className="flex h-12 items-center laptop:h-15 desktop:h-15">
        <div className="flex grow items-center px-4">
          <div className="flex grow items-center">
            {(isPhone || isTablet) && (
              <Pause
                className="mr-2 text-text-primary hover:cursor-pointer"
                onClick={openMenu}
              />
            )}
            <LocationSelector />
          </div>
          {!isPhone && headerWidgetEnabled && <HeaderWidgets />}
        </div>
      </div>
      {showPhoneWidgets && (
        <div
          className={clsx(
            "flex h-[57px] items-center",
            isDashboard ? "bg-surface-5" : "bg-surface-base",
          )}
        >
          <HeaderWidgets className="ml-5" />
        </div>
      )}
    </>
  );
};

export default PageHeader;
