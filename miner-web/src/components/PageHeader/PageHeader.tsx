import { useWindowDimensions } from "common/hooks/useWindowDimensions";

import { Pause } from "icons";

import MinerStatus from "./MinerStatus";
import PoolStatus from "./PoolStatus";
import PowerWidget from "./Power";

interface PageHeaderProps {
  openMenu?: () => void;
  title: string;
}

const PageHeader = ({ openMenu, title }: PageHeaderProps) => {
  const { isPhone, isTablet } = useWindowDimensions();
  return (
    <div className="h-[60px] flex border-b border-border-primary/5 items-center">
      <div className="flex grow px-4 items-center">
        <div className="flex grow">
          {(isPhone || isTablet) && (
            <Pause
              className="mr-2 text-text-primary hover:cursor-pointer"
              onClick={openMenu}
            />
          )}
          <div className="text-300 text-text-primary/70">{title}</div>
        </div>
        <div className="flex space-x-3">
          <PowerWidget />
          <PoolStatus />
          <MinerStatus />
        </div>
      </div>
    </div>
  );
};

export default PageHeader;
