import { useCallback } from "react";
import clsx from "clsx";

import { useNetworkInfo } from "api";

import Divider from "components/Divider";
import Row from "components/Row";

import { tabs } from "../constants";
import { Tabs } from "../types";

interface NavigationProps {
  activeTab: Tabs;
  poolUrls?: string[];
  onChangeActiveTab: (tab: Tabs) => void;
  onItemClick?: () => void;
}

const Navigation = ({
  activeTab,
  poolUrls = [],
  onChangeActiveTab,
  onItemClick,
}: NavigationProps) => {
  const { data: networkInfo } = useNetworkInfo();

  const handleClick = useCallback(
    (newActiveTab: Tabs) => {
      onChangeActiveTab(newActiveTab);
      onItemClick?.();
    },
    [onChangeActiveTab, onItemClick]
  );

  return (
    <div
      className={clsx(
        "border-r border-border-primary/5 flex flex-col fixed bg-surface-base z-30",
        "desktop:w-80 laptop:w-80 tablet:w-60 phone:w-60",
        "desktop:h-[calc(100vh-66px)] laptop:h-[calc(100vh-66px)] tablet:h-[calc(100vh-16px)] phone:h-[calc(100vh-16px)]",
        "tablet:-mt-[66px] phone:-mt-[66px]",
      )}
    >
      <div className="flex-1 p-6">
        <div className="mb-3">
          <div className="text-heading-200 mb-1">Miner setup</div>
          <div className="text-300 text-text-primary/70">
            {networkInfo?.mac}
          </div>
        </div>
        <div className="text-emphasis-300 text-text-primary">
          <Row
            className={clsx({
              "text-text-primary/30 hover:text-text-primary":
                activeTab !== tabs.pools,
            })}
            onClick={() => handleClick(tabs.pools)}
            testId="pools-tab"
          >
            <div>Pools</div>
            {poolUrls.map((poolUrl, index) => (
              <div className="text-200" key={index}>
                {poolUrl}
              </div>
            ))}
          </Row>
          <Row
            className={clsx("", {
              "text-text-primary/30": activeTab !== tabs.cooling,
              "hover:cursor-not-allowed": !poolUrls.length,
            })}
            {...(poolUrls.length && {
              onClick: () => handleClick(tabs.cooling),
            })}
            testId="cooling-tab"
          >
            Cooling
          </Row>
        </div>
      </div>
      <div>
        <Divider />
        <div className="p-6 pt-4">
          {/* TODO: add documentation and support links when available */}
          <Row compact className="text-200 text-text-primary/70">
            Documentation
          </Row>
          <Row
            compact
            className="text-200 text-text-primary/70"
            divider={false}
          >
            Support
          </Row>
        </div>
      </div>
    </div>
  );
};

export default Navigation;
