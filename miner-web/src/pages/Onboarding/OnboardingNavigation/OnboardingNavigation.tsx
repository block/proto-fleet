import clsx from "clsx";

import { useNetworkInfo } from "api";

import Divider from "components/Divider";
import Row from "components/Row";

import { tabs } from "../constants";
import { Tabs } from "../types";

interface OnboardingNavigationProps {
  activeTab: Tabs;
  poolUrls?: string[];
  onChangeActiveTab: (tab: Tabs) => void;
}

const OnboardingNavigation = ({
  activeTab,
  poolUrls = [],
  onChangeActiveTab,
}: OnboardingNavigationProps) => {
  const { data: networkInfo } = useNetworkInfo();

  return (
    <div className="w-80 border-r border-border-primary/5 flex flex-col fixed h-[calc(100vh-66px)] bg-surface-base">
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
            onClick={() => onChangeActiveTab(tabs.pools)}
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
              onClick: () => onChangeActiveTab(tabs.cooling),
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

export default OnboardingNavigation;
