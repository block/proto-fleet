import { ReactNode } from "react";

import FirmwareUpdateStatus from "./FirmwareUpdateStatus";
import MinerStatus from "./MinerStatus";
import PoolStatus from "./PoolStatus";
import PowerWidget from "./Power";
import PowerTarget from "./PowerTarget";
import { Pause } from "@/shared/assets/icons";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

type CustomButtons =
  | ReactNode
  | { left: ReactNode; right: ReactNode }
  | { right: ReactNode }
  | { left: ReactNode };
interface PageHeaderProps {
  customButtons?: CustomButtons;
  openMenu?: () => void;
  title: string;
}

function getCustomButtons(
  customButtons: CustomButtons | undefined,
): [ReactNode | undefined, ReactNode | undefined] {
  let customLeftButtons: ReactNode | undefined;
  let customRightButtons: ReactNode | undefined;

  if (
    customButtons &&
    typeof customButtons === "object" &&
    "left" in customButtons
  ) {
    customLeftButtons = customButtons.left;
  }
  if (
    customButtons &&
    typeof customButtons === "object" &&
    "right" in customButtons
  ) {
    customRightButtons = customButtons.right;
  }
  if (
    customButtons &&
    typeof customButtons === "object" &&
    !("left" in customButtons) &&
    !("right" in customButtons)
  ) {
    customRightButtons = customButtons;
  }
  return [customLeftButtons, customRightButtons];
}

const PageHeader = ({ customButtons, openMenu, title }: PageHeaderProps) => {
  const { isPhone, isTablet } = useWindowDimensions();

  const [customLeftButtons, customRightButtons] =
    getCustomButtons(customButtons);

  return (
    <div className="fixed top-0 right-0 left-0 z-20 flex h-[60px] border-b border-border-5 bg-surface-base phone:h-fit phone:py-2 tablet:h-fit tablet:py-2">
      <div className="flex w-full items-center justify-end gap-4 pl-60 phone:flex-col phone:justify-start phone:px-0 tablet:flex-col tablet:px-0">
        {(isPhone || isTablet) && (
          <div className="flex grow items-center gap-2 self-start px-4">
            <Pause
              className="text-text-primary hover:cursor-pointer"
              onClick={openMenu}
            />
            <div className="text-300 text-text-primary-70">{title}</div>
          </div>
        )}
        <div className="flex grow justify-between space-x-3 self-center px-4 [scrollbar-width:none] phone:w-full phone:justify-start phone:self-end phone:overflow-x-auto phone:py-[1px] tablet:w-full tablet:justify-start">
          <div className="flex space-x-3 phone:flex-shrink-0">
            {customLeftButtons ?? (
              <>
                <MinerStatus />
                <FirmwareUpdateStatus />
              </>
            )}
          </div>
          <div className="flex space-x-3 phone:flex-shrink-0">
            {customRightButtons ?? (
              <>
                <PowerTarget />
                <PoolStatus />
                <PowerWidget />
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default PageHeader;
