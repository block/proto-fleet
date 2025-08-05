import clsx from "clsx";
import AlertStatus from "./AlertStatus";
import Button, { variants, sizes } from "@/shared/components/Button";
import LocationSelector from "./LocationSelector";
import { Pause } from "@/shared/assets/icons";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";
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
          variant={variants.accent}
          size={sizes.compact}
          text="Complete Setup"
          onClick={handleCompleteSetup}
        />
      )}
    </div>
  );
};

const PageHeader = ({ openMenu }: PageHeaderProps) => {
  const { isPhone, isTablet } = useWindowDimensions();

  return (
    <>
      <div className="flex h-12 items-center border-b border-border-5 laptop:h-15 desktop:h-15">
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
      {isPhone && (
        <div className="flex h-[57px] items-center border-b border-border-5">
          <HeaderWidgets className="ml-5" />
        </div>
      )}
    </>
  );
};

export default PageHeader;
