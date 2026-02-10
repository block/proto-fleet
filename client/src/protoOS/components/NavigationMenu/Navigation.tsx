import { useCallback, useMemo } from "react";
import { Link, useLocation } from "react-router-dom";
import clsx from "clsx";

import { navigationItems, navigationMenuTypes } from "./constants";
import IpAddressInfo from "./InfoItem/IpAddressInfo";
import type { IpAddressInfoProps } from "./InfoItem/IpAddressInfo";
import MacAddressInfo from "./InfoItem/MacAddressInfo";
import type { MacAddressInfoProps } from "./InfoItem/MacAddressInfo";
import MinerNameInfo from "./InfoItem/MinerNameInfo";
import type { MinerNameInfoProps } from "./InfoItem/MinerNameInfo";
import VersionInfo from "./InfoItem/VersionInfo";
import type { VersionInfoProps } from "./InfoItem/VersionInfo";
import { AppNavigationItems, OnboardingNavigationItems } from "./NavigationItems";
import { NavigationItemValue, NavigationMenuType } from "./types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { Logo } from "@/shared/assets/icons";
import { useNavigate } from "@/shared/hooks/useNavigate";

interface NavigationProps {
  ipAddressInfo?: IpAddressInfoProps;
  macInfo?: MacAddressInfoProps;
  minerNameInfo?: MinerNameInfoProps;
  onItemClick?: () => void;
  versionInfo?: VersionInfoProps;
  type: NavigationMenuType;
}

const Navigation = ({ ipAddressInfo, macInfo, minerNameInfo, onItemClick, versionInfo, type }: NavigationProps) => {
  const isApp = useMemo(() => type === navigationMenuTypes.app, [type]);

  const { minerRoot, closeButton } = useMinerHosting();

  const isOnboarding = useMemo(() => type === navigationMenuTypes.onboarding, [type]);

  const navigate = useNavigate();
  const location = useLocation();
  const { pathname } = useMemo(() => location, [location]);
  const pageName = useMemo(() => {
    // Remove leading slash
    const route = pathname.replace(/^\//, "");
    if (route.length) {
      return route;
    } else {
      return isApp ? navigationItems.home : navigationItems.onboarding;
    }
  }, [pathname, isApp]);

  const handleClick = useCallback(
    (navigationItem: NavigationItemValue) => {
      navigate(`${minerRoot}/${navigationItem}`);
      onItemClick?.();
    },
    [onItemClick, navigate, minerRoot],
  );

  return (
    <div
      className={clsx(
        "flex h-full max-h-screen w-[240px] flex-col border-r border-border-5 bg-surface-base text-text-primary-70",
        "tablet:absolute tablet:z-30 tablet:max-h-[calc(100vh-16px)] tablet:rounded-lg",
        "overflow-auto phone:absolute phone:z-30 phone:max-h-[calc(100vh-16px)] phone:rounded-lg",
      )}
    >
      <div className="grow">
        <div className="mb-3 flex h-[60px] items-center px-3 py-2">
          {closeButton ? (
            closeButton
          ) : (
            <Link to={isApp ? `${minerRoot}/${navigationItems.home}` : `${minerRoot}/${navigationItems.onboarding}`}>
              <Logo className="text-text-primary hover:cursor-pointer" />
            </Link>
          )}
        </div>
        <div className="px-3" data-testid="navigation">
          {isApp && <AppNavigationItems pageName={pageName} onClick={handleClick} />}
          {isOnboarding && <OnboardingNavigationItems pageName={pageName} onClick={handleClick} />}
        </div>
      </div>

      <div className="px-3 pb-3">
        <MinerNameInfo loading={minerNameInfo?.loading} value={minerNameInfo?.value} />

        <IpAddressInfo loading={ipAddressInfo?.loading} value={ipAddressInfo?.value} />

        <VersionInfo loading={versionInfo?.loading} value={versionInfo?.value} />

        <MacAddressInfo loading={macInfo?.loading} value={macInfo?.value} />
      </div>
    </div>
  );
};

export default Navigation;
