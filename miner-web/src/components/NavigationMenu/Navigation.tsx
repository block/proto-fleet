import { useCallback, useMemo } from "react";
import { Link, useLocation } from "react-router-dom";
import clsx from "clsx";

import { useNavigate } from "common/hooks/useNavigate";
import Row from "components/Row";

import { Logo } from "icons";

import { navigationItems, navigationMenuTypes } from "./constants";
import MacAddressInfo, { MacAddressInfoProps } from "./InfoItem/MacAddressInfo";
import VersionInfo, { VersionInfoProps } from "./InfoItem/VersionInfo";
import {
  AppNavigationItems,
  OnboardingNavigationItems,
} from "./NavigationItems";
import { NavigationItemValue, NavigationMenuType } from "./types";

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

  const isOnboarding = useMemo(
    () => type === navigationMenuTypes.onboarding,
    [type]
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
      navigate(`/${navigationItem}`);
      onItemClick?.();
    },
    [onItemClick, navigate]
  );

  return (
    <div
      className={clsx(
        "w-[240px] min-h-screen flex flex-col bg-surface-base text-text-primary-70 border-r border-border-5",
        "tablet:min-h-[calc(100vh-16px)] tablet:z-30 tablet:absolute tablet:rounded-lg",
        "phone:min-h-[calc(100vh-16px)] phone:z-30 phone:absolute phone:rounded-lg"
      )}
    >
      <div className="grow border-b border-border-5">
        <div className="h-[60px] px-3 py-2 flex items-center border-b border-border-5 mb-3">
          <Link
            to={
              isApp
                ? `/${navigationItems.home}`
                : `/${navigationItems.onboarding}`
            }
          >
            <Logo className="hover:cursor-pointer text-text-primary" />
          </Link>
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

      <div className="px-3 pb-1">
        <VersionInfo
          loading={versionInfo?.loading}
          value={versionInfo?.value}
        />
        <MacAddressInfo loading={macInfo?.loading} value={macInfo?.value} />
        <Row compact className="text-200 text-text-primary-70">
          <a href="https://proto.xyz/docs/api/v1.1.0" target="_blank">
            API Documentation
          </a>
        </Row>
        <Row compact className="text-200 text-text-primary-70" divider={false}>
          <a href="mailto:mining.support@block.xyz" target="_blank">
            Contact us
          </a>
        </Row>
      </div>
    </div>
  );
};

export default Navigation;
