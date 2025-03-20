import { ReactNode } from "react";

import MinerStatus from "./MinerStatus";
import PoolStatus from "./PoolStatus";
import PowerWidget from "./Power";
import { Pause } from "@/shared/assets/icons";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface PageHeaderProps {
  customButtons?: ReactNode;
  openMenu?: () => void;
  title: string;
}

const PageHeader = ({ customButtons, openMenu, title }: PageHeaderProps) => {
  const { isPhone, isTablet } = useWindowDimensions();
  return (
    <div className="fixed top-0 left-0 right-0 z-20 bg-surface-base h-[60px] flex border-b border-border-5 items-center">
      <div className="flex grow px-4 items-center">
        <div className="flex grow">
          {(isPhone || isTablet) && (
            <Pause
              className="mr-2 text-text-primary hover:cursor-pointer"
              onClick={openMenu}
            />
          )}
          <div className="text-300 text-text-primary-70">{title}</div>
        </div>
        <div className="flex space-x-3">
          {customButtons || (
            <>
              <PowerWidget />
              <PoolStatus />
              <MinerStatus />
            </>
          )}
        </div>
      </div>
    </div>
  );
};

export default PageHeader;
