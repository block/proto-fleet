import { useNavigate } from "react-router-dom";

import { useWindowDimensions } from "common/hooks/useWindowDimensions";

import { Pause, Terminal } from "icons";

import PoolStatus from "./PoolStatus";
import PowerWidget from "./Power";
// import Warning from "./Warning";

interface PageHeaderProps {
  openMenu?: () => void;
  title: string;
}

const PageHeader = ({ openMenu, title }: PageHeaderProps) => {
  const { isPhone, isTablet } = useWindowDimensions();
  const navigate = useNavigate();

  const handleClickTerminal = () => {
    navigate("/logs");
  };

  return (
    <div className="h-[56px] flex border-b border-border-primary/5 items-center">
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
          {/* TODO: add errors & warnings from API when available */}
          {/* <Warning errorCount={47} errorType="asic" state="critical" />
          <Warning errorCount={12} errorType="fan" state="warning" /> */}
        </div>
      </div>
      <div className="border-l-[1px] border-border-primary/5 px-5 h-full flex items-center">
        <button onClick={handleClickTerminal}>
          <Terminal />
        </button>
      </div>
    </div>
  );
};

export default PageHeader;
