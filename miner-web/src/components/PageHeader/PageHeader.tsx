import { useWindowDimensions } from "common/hooks/useWindowDimensions";

import { Pause } from "icons";

import PoolStatus from "./PoolStatus";
// import Warning from "./Warning";

interface PageHeaderProps {
  openMenu?: () => void;
  title: string;
}

const PageHeader = ({ openMenu, title }: PageHeaderProps) => {
  const { isPhone, isTablet } = useWindowDimensions();
  return (
    <div className="h-[56px] flex border-b border-border-primary/5 py-2 p-[15px] items-center">
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
        <PoolStatus />
        {/* TODO: add errors & warnings from API when available */}
        {/* <Warning errorCount={47} errorType="asic" state="critical" />
        <Warning errorCount={12} errorType="fan" state="warning" /> */}
      </div>
    </div>
  );
};

export default PageHeader;
