import { useEffect, useMemo, useState } from "react";
import { useLocation } from "react-router-dom";

import { Home, Mining, Settings } from "icons";

import { navigationItems } from "./constants";
import MacAddressInfo, {
  MacAddressInfoProps,
} from "./InfoItems/MacAddressInfo";
import NavigationItem from "./NavigationItem";

interface NavigationProps {
  macInfo?: MacAddressInfoProps;
}

const Navigation = ({ macInfo }: NavigationProps) => {
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

      <MacAddressInfo loading={macInfo?.loading} value={macInfo?.value} />
    </div>
  );
};

export default Navigation;
