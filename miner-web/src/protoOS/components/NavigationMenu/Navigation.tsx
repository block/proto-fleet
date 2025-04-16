import { useCallback, useMemo } from "react";
import { Link, useLocation } from "react-router-dom";
import clsx from "clsx";

import { navigationItems, navigationMenuTypes } from "./constants";
import MacAddressInfo, { MacAddressInfoProps } from "./InfoItem/MacAddressInfo";
import VersionInfo, { VersionInfoProps } from "./InfoItem/VersionInfo";
import {
  AppNavigationItems,
  OnboardingNavigationItems,
} from "./NavigationItems";
import { NavigationItemValue, NavigationMenuType } from "./types";
import { useMinerHosting } from "@/protoOS/api";
import { Logo } from "@/shared/assets/icons";
import { useNavigate } from "@/shared/hooks/useNavigate";

interface NavigationProps {
  macInfo?: MacAddressInfoProps;
  onItemClick?: () => void;
  versionInfo?: VersionInfoProps;
  type: NavigationMenuType;
}

const Navigation = ({
  macInfo,
  onItemClick,
  versionInfo,
  type,
}: NavigationProps) => {
  const isApp = useMemo(() => type === navigationMenuTypes.app, [type]);

  const { minerRoot, closeButton } = useMinerHosting();

  const isOnboarding = useMemo(
    () => type === navigationMenuTypes.onboarding,
    [type],
  );

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
        "flex min-h-screen w-[240px] flex-col border-r border-border-5 bg-surface-base text-text-primary-70",
        "tablet:absolute tablet:z-30 tablet:min-h-[calc(100vh-16px)] tablet:rounded-lg",
        "phone:absolute phone:z-30 phone:min-h-[calc(100vh-16px)] phone:rounded-lg",
      )}
    >
      <div className="grow border-b border-border-5">
        <div className="mb-3 flex h-[60px] items-center border-b border-border-5 px-3 py-2">
          {closeButton ? (
            closeButton
          ) : (
            <Link
              to={
                isApp
                  ? `${minerRoot}/${navigationItems.home}`
                  : `${minerRoot}/${navigationItems.onboarding}`
              }
            >
              <Logo className="text-text-primary hover:cursor-pointer" />
            </Link>
          )}
        </div>
        <div className="px-3">
          {isApp && (
            <AppNavigationItems pageName={pageName} onClick={handleClick} />
          )}
          {isOnboarding && (
            <OnboardingNavigationItems
              pageName={pageName}
              onClick={handleClick}
            />
          )}
        </div>
      </div>

      <div className="px-3 pb-3">
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
