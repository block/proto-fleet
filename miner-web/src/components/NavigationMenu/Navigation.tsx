import { useEffect, useMemo, useState } from "react";
import { useLocation } from "react-router-dom";
import clsx from "clsx";

import { Home, Mining, Settings } from "icons";

import { navigationItems } from "./constants";
import MacAddressInfo, { MacAddressInfoProps } from "./MacAddressInfo";
import NavigationItem from "./NavigationItem";

interface NavigationProps {
  macInfo?: MacAddressInfoProps;
  onItemClick?: () => void;
}

const Navigation = ({ macInfo, onItemClick }: NavigationProps) => {
  const location = useLocation();
  const { pathname } = useMemo(() => location, [location]);
  const pageName = useMemo(() => {
    const newPageName = pathname.split("/")[1];
    if (newPageName in navigationItems) {
      return newPageName as keyof typeof navigationItems;
    }
    return navigationItems.home;
  }, [pathname]);

  const [selected, setSelected] = useState(navigationItems[pageName]);

  useEffect(() => {
    setSelected(navigationItems[pageName]);
  }, [pageName]);

  const handleClick = (navigationItem: keyof typeof navigationItems) => {
    setSelected(navigationItem);
    onItemClick?.();
  };

  return (
    <div
      className={clsx(
        "w-[240px] min-h-screen p-3 flex flex-col bg-core-primary-fill text-text-contrast/70",
        "tablet:min-h-[calc(100vh-16px)] tablet:z-30 tablet:absolute tablet:rounded-lg",
        "phone:min-h-[calc(100vh-16px)] phone:z-30 phone:absolute phone:rounded-lg"
      )}
    >
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
          onClick={handleClick}
        />
        <NavigationItem
          icon={<Mining />}
          id={navigationItems.hardware}
          text="Hardware"
          selected={selected}
          onClick={handleClick}
        />
        <NavigationItem
          icon={<Settings />}
          id={navigationItems.settings}
          text="Settings"
          selected={selected}
          onClick={handleClick}
        />
      </div>

      <MacAddressInfo loading={macInfo?.loading} value={macInfo?.value} />
    </div>
  );
};

export default Navigation;
