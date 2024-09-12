import { useCallback, useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import clsx from "clsx";

import { Logo, Minus, Plus } from "icons";

import { navigationItems } from "./constants";
import MacAddressInfo, { MacAddressInfoProps } from "./InfoItem/MacAddressInfo";
import VersionInfo, { VersionInfoProps } from "./InfoItem/VersionInfo";
import NavigationItem from "./NavigationItem";
import { NavigationItemValue } from "./types";

interface NavigationProps {
  macInfo?: MacAddressInfoProps;
  onItemClick?: () => void;
  versionInfo?: VersionInfoProps;
}

const Navigation = ({ macInfo, onItemClick, versionInfo }: NavigationProps) => {
  const navigate = useNavigate();
  const location = useLocation();
  const { pathname } = useMemo(() => location, [location]);
  const pageName = useMemo(() => {
    // Remove leading slash
    const route = pathname.replace(/^\//, "");
    if (route.length) {
      return route;
    } else {
      return "home";
    }
  }, [pathname]);
  const [showAccordionItems, setShowAccordionItems] = useState(
    pageName.startsWith("settings")
  );
  const [showAccordionExpand, setShowAccordionExpand] = useState(false);

  const handleClick = useCallback(
    (navigationItem: NavigationItemValue) => {
      navigate(`/${navigationItem}`);
      onItemClick?.();
    },
    [onItemClick, navigate]
  );

  const handleAccordionClick = useCallback(() => {
    setShowAccordionItems((prev) => !prev);
  }, []);

  const handleAccordionHover = useCallback((hover: boolean) => {
    setShowAccordionExpand(hover);
  }, []);

  return (
    <div
      className={clsx(
        "w-[240px] min-h-screen flex flex-col bg-surface-base text-text-primary/70 border-r border-border-primary/5",
        "tablet:min-h-[calc(100vh-16px)] tablet:z-30 tablet:absolute tablet:rounded-lg",
        "phone:min-h-[calc(100vh-16px)] phone:z-30 phone:absolute phone:rounded-lg"
      )}
    >
      <div className="grow border-b border-border-primary/5">
        <div className="h-[60px] px-3 py-2 flex items-center border-b border-border-primary/5 mb-3">
          <Logo />
        </div>
        <div className="px-3">
          <NavigationItem
            id={navigationItems.home}
            text="Home"
            onClick={handleClick}
            pageName={pageName}
          />
          <NavigationItem
            id={navigationItems.temperature}
            text="Temperature"
            onClick={handleClick}
            pageName={pageName}
          />
          <NavigationItem
            id={navigationItems.logs}
            text="Logs"
            onClick={handleClick}
            pageName={pageName}
          />
          <NavigationItem
            suffixIcon={
              showAccordionExpand || showAccordionItems ? (
                showAccordionExpand && !showAccordionItems ? (
                  <Plus />
                ) : (
                  <Minus />
                )
              ) : undefined
            }
            text="Settings"
            onClick={handleAccordionClick}
            onHover={handleAccordionHover}
          />
          {showAccordionItems && (
            <>
              <NavigationItem
                id={navigationItems.miningPools}
                text="Mining Pools"
                onClick={handleClick}
                pageName={pageName}
                isChildItem
              />
            </>
          )}
        </div>
      </div>

      <div className="px-3 pb-1">
        <VersionInfo
          loading={versionInfo?.loading}
          value={versionInfo?.value}
        />
        <MacAddressInfo loading={macInfo?.loading} value={macInfo?.value} />
      </div>
    </div>
  );
};

export default Navigation;
