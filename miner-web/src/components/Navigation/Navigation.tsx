import { useState } from "react";
import { useLocation } from "react-router-dom";

import { Home, Mining, Settings } from "icons";

import { navigationItems } from "./constants";
import MacAddressInfo, { MacAddressInfoProps } from "./InfoItems/MacAddressInfo";
import PoolInfo, { PoolProps } from "./InfoItems/PoolInfo";
import NavigationItem from "./NavigationItem";

interface NavigationProps {
  macInfo?: MacAddressInfoProps;
  poolInfo?: PoolProps;
}

const Navigation = ({ macInfo, poolInfo }: NavigationProps) => {
  const location = useLocation();
  const { pathname } = location;
  const pageName = pathname.split("/")[1] as keyof typeof navigationItems;

  const [selected, setSelected] = useState(
    (navigationItems[pageName] ||
      navigationItems.home) as keyof typeof navigationItems
  );

  return (
    <div className="w-[216px] h-auto min-h-screen p-3 flex flex-col bg-core-primary-fill text-text-contrast/70">
      <div className="grow">
        {/* TODO: replace with logo when ready */}
        <div className="text-[18px] font-semibold text-text-contrast py-2 mb-3">
          Proto
        </div>
        <NavigationItem
          icon={<Home />}
          id={navigationItems.home}
          text="Home"
          selected={selected}
          setSelected={setSelected}
        />
        <NavigationItem
          icon={<Mining />}
          id={navigationItems.hardware}
          text="Hardware"
          selected={selected}
          setSelected={setSelected}
        />
        <NavigationItem
          icon={<Settings />}
          id={navigationItems.settings}
          text="Settings"
          selected={selected}
          setSelected={setSelected}
        />
      </div>

      <PoolInfo
        status={poolInfo?.status}
        url={poolInfo?.url}
        loading={poolInfo?.loading}
        error={poolInfo?.error}
      />
      <MacAddressInfo
        loading={macInfo?.loading}
        value={macInfo?.value}
      />
    </div>
  );
};

export default Navigation;
