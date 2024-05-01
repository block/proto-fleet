import { useCallback, useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import clsx from "clsx";

import { Caret, Home, Mining, Settings } from "icons";

import { navigationItems } from "./constants";
import MacAddressInfo, { MacAddressInfoProps } from "./MacAddressInfo";
import NavigationItem from "./NavigationItem";
import { NavigationItemValue } from "./types";

interface NavigationProps {
  macInfo?: MacAddressInfoProps;
  onItemClick?: () => void;
}

const Navigation = ({ macInfo, onItemClick }: NavigationProps) => {
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
  const [showAccordionCaret, setShowAccordionCaret] = useState(false);

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
    setShowAccordionCaret(hover);
  }, []);

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
          onClick={handleClick}
          pageName={pageName}
        />
        <NavigationItem
          icon={<Mining />}
          id={navigationItems.hardware}
          text="Hardware"
          onClick={handleClick}
          pageName={pageName}
        />
        <NavigationItem
          icon={<Settings />}
          suffixIcon={
            showAccordionCaret || showAccordionItems ? (
              <Caret
                className={clsx({
                  "-rotate-90": showAccordionCaret && !showAccordionItems,
                })}
              />
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
            />
            <NavigationItem
              id={navigationItems.cooling}
              text="Cooling"
              onClick={handleClick}
              pageName={pageName}
            />
          </>
        )}
      </div>

      <MacAddressInfo loading={macInfo?.loading} value={macInfo?.value} />
    </div>
  );
};

export default Navigation;
